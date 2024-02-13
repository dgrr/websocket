package websocket

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/valyala/bytebufferpool"
	"github.com/valyala/fasthttp"
)

type (
	// OpenHandler handles when a connection is open.
	OpenHandler func(c *Conn)
	// PingHandler handles the data from a ping frame.
	PingHandler func(c *Conn, data []byte)
	// PongHandler receives the data from a pong frame.
	PongHandler func(c *Conn, data []byte)
	// MessageHandler receives the payload content of a data frame
	// indicating whether the content is binary or not.
	MessageHandler func(c *Conn, isBinary bool, data []byte)
	// FrameHandler receives the raw frame. This handler is optional,
	// if none is specified the server will run a default handler.
	//
	// If the user specifies a FrameHandler, then it is going to receive all incoming frames.
	FrameHandler func(c *Conn, fr *Frame)
	// CloseHandler fires when a connection has been closed.
	CloseHandler func(c *Conn, err error)
	// ErrorHandler fires when an unknown error happens.
	ErrorHandler func(c *Conn, err error)
)

// Server represents the WebSocket server.
//
// Server is going to be in charge of upgrading the connection, is not a server per-se.
type Server struct {
	// UpgradeHandler allows the user to handle RequestCtx when upgrading for fasthttp.
	//
	// If UpgradeHandler returns false the connection won't be upgraded.
	UpgradeHandler UpgradeHandler

	// UpgradeHandler allows the user to handle the request when upgrading for net/http.
	//
	// If UpgradeNetHandler returns false, the connection won't be upgraded.
	UpgradeNetHandler UpgradeNetHandler

	// Protocols are the supported protocols.
	Protocols []string

	// Origin is used to limit the clients coming from the defined origin
	Origin string

	nextID uint64

	openHandler  OpenHandler
	frHandler    FrameHandler
	closeHandler CloseHandler
	msgHandler   MessageHandler
	pingHandler  PingHandler
	pongHandler  PongHandler
	errHandler   ErrorHandler

	once sync.Once
}

func (s *Server) initServer() {
	if s.frHandler != nil {
		return
	}

	s.frHandler = s.handleFrame
}

// HandleData sets the MessageHandler.
func (s *Server) HandleData(msgHandler MessageHandler) {
	s.msgHandler = msgHandler
}

// HandleOpen sets a callback for handling opening connections.
func (s *Server) HandleOpen(openHandler OpenHandler) {
	s.openHandler = openHandler
}

// HandleClose sets a callback for handling connection close.
func (s *Server) HandleClose(closeHandler CloseHandler) {
	s.closeHandler = closeHandler
}

// HandlePing sets a callback for handling the data of the ping frames.
//
// The server is in charge of replying to the PING frames, thus the client
// MUST not reply to any control frame.
func (s *Server) HandlePing(pingHandler PingHandler) {
	s.pingHandler = pingHandler
}

// HandlePong sets a callback for handling the data of the pong frames.
func (s *Server) HandlePong(pongHandler PongHandler) {
	s.pongHandler = pongHandler
}

// HandleError ...
func (s *Server) HandleError(errHandler ErrorHandler) {
	s.errHandler = errHandler
}

// HandleFrame sets a callback for handling all the incoming Frames.
//
// If none is specified, the server will run a default handler.
func (s *Server) HandleFrame(frameHandler FrameHandler) {
	s.frHandler = frameHandler
}

// Upgrade upgrades websocket connections.
func (s *Server) Upgrade(ctx *fasthttp.RequestCtx) {
	if !ctx.IsGet() {
		ctx.SetStatusCode(fasthttp.StatusBadRequest)
		return
	}

	s.once.Do(s.initServer)

	// Checking Origin header if needed
	origin := ctx.Request.Header.Peek("Origin")
	if s.Origin != "" {
		uri := fasthttp.AcquireURI()
		uri.Update(s.Origin)

		b := bytePool.Get().([]byte)
		b = prepareOrigin(b, uri)
		fasthttp.ReleaseURI(uri)

		if !equalsFold(b, origin) {
			ctx.SetStatusCode(fasthttp.StatusForbidden)
			bytePool.Put(b)
			return
		}

		bytePool.Put(b)
	}

	// Normalizing must be disabled because of WebSocket header fields.
	// (This is not a fasthttp bug).
	ctx.Response.Header.DisableNormalizing()

	// Connection.Value == Upgrade
	if ctx.Request.Header.ConnectionUpgrade() {
		// Peek sade header field.
		hup := ctx.Request.Header.PeekBytes(upgradeString)
		// Compare with websocket string defined by the RFC
		if equalsFold(hup, websocketString) {
			// Checking websocket version
			hversion := ctx.Request.Header.PeekBytes(wsHeaderVersion)

			// Peeking websocket key.
			hkey := ctx.Request.Header.PeekBytes(wsHeaderKey)
			hprotos := bytes.Split( // TODO: Reduce allocations. Do not split. Use IndexByte
				ctx.Request.Header.PeekBytes(wsHeaderProtocol), commaString,
			)

			supported := false
			// Checking versions
			for i := range supportedVersions {
				if bytes.Contains(supportedVersions[i], hversion) {
					supported = true
					break
				}
			}

			if !supported {
				ctx.Error("Versions not supported", fasthttp.StatusBadRequest)
				return
			}

			if s.UpgradeHandler != nil {
				if !s.UpgradeHandler(ctx) {
					return
				}
			}
			// TODO: compression
			// compress := mustCompress(exts)

			// Setting response headers
			ctx.Response.SetStatusCode(fasthttp.StatusSwitchingProtocols)
			ctx.Response.Header.AddBytesKV(connectionString, upgradeString)
			ctx.Response.Header.AddBytesKV(upgradeString, websocketString)
			ctx.Response.Header.AddBytesKV(wsHeaderAccept, makeKey(hkey, hkey))

			// TODO: implement bad websocket version
			// https://tools.ietf.org/html/rfc6455#section-4.4
			if proto := selectProtocol(hprotos, s.Protocols); proto != "" {
				ctx.Response.Header.AddBytesK(wsHeaderProtocol, proto)
			}

			nctx := context.Background()
			ctx.VisitUserValues(func(k []byte, v interface{}) {
				nctx = context.WithValue(nctx, string(k), v)
			})

			ctx.Hijack(func(c net.Conn) {
				if nc, ok := c.(interface {
					UnsafeConn() net.Conn
				}); ok {
					c = nc.UnsafeConn()
				}

				conn := acquireConn(c)
				conn.id = atomic.AddUint64(&s.nextID, 1)
				// establishing default options
				conn.ctx = nctx

				if s.openHandler != nil {
					s.openHandler(conn)
				}

				s.serveConn(conn)
			})
		}
	}
}

// NetUpgrade upgrades the websocket connection for net/http.
func (s *Server) NetUpgrade(resp http.ResponseWriter, req *http.Request) {
	if req.Method != "GET" {
		resp.WriteHeader(http.StatusBadRequest)
		return
	}

	rs := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseResponse(rs)

	s.once.Do(s.initServer)

	// Checking Origin header if needed
	origin := req.Header.Get("Origin")
	if s.Origin != "" {
		uri := fasthttp.AcquireURI()
		uri.Update(s.Origin)

		b := bytePool.Get().([]byte)
		b = prepareOrigin(b, uri)
		fasthttp.ReleaseURI(uri)

		if !equalsFold(b, s2b(origin)) {
			resp.WriteHeader(http.StatusForbidden)
			bytePool.Put(b)
			return
		}

		bytePool.Put(b)
	}

	// Normalizing must be disabled because of WebSocket header fields.
	// (This is not a fasthttp bug).
	rs.Header.DisableNormalizing()

	hasUpgrade := func() bool {
		for _, v := range req.Header["Connection"] {
			if strings.Contains(v, "Upgrade") {
				return true
			}
		}
		return false
	}()

	// Connection.Value == Upgrade
	if hasUpgrade {
		// Peek sade header field.
		hup := req.Header.Get("Upgrade")
		// Compare with websocket string defined by the RFC
		if equalsFold(s2b(hup), websocketString) {
			// Checking websocket version
			hversion := req.Header.Get(b2s(wsHeaderVersion))
			// Peeking websocket key.
			hkey := req.Header.Get(b2s(wsHeaderKey))
			hprotos := bytes.Split( // TODO: Reduce allocations. Do not split. Use IndexByte
				s2b(req.Header.Get(b2s(wsHeaderProtocol))), commaString,
			)
			supported := false
			// Checking versions
			for i := range supportedVersions {
				if bytes.Contains(supportedVersions[i], s2b(hversion)) {
					supported = true
					break
				}
			}
			if !supported {
				resp.WriteHeader(http.StatusBadRequest)
				io.WriteString(resp, "Versions not supported")
				return
			}

			if s.UpgradeNetHandler != nil {
				if !s.UpgradeNetHandler(resp, req) {
					return
				}
			}
			// TODO: compression

			h, ok := resp.(http.Hijacker)
			if !ok {
				resp.WriteHeader(http.StatusInternalServerError)
				return
			}

			c, _, err := h.Hijack()
			if err != nil {
				io.WriteString(resp, err.Error())
				return
			}

			// Setting response headers
			rs.SetStatusCode(fasthttp.StatusSwitchingProtocols)
			rs.Header.AddBytesKV(connectionString, upgradeString)
			rs.Header.AddBytesKV(upgradeString, websocketString)
			rs.Header.AddBytesKV(wsHeaderAccept, makeKey(s2b(hkey), s2b(hkey)))
			// TODO: implement bad websocket version
			// https://tools.ietf.org/html/rfc6455#section-4.4
			if proto := selectProtocol(hprotos, s.Protocols); proto != "" {
				rs.Header.AddBytesK(wsHeaderProtocol, proto)
			}

			_, err = rs.WriteTo(c)
			if err != nil {
				c.Close()
				return
			}

			go func(ctx context.Context) {
				conn := acquireConn(c)
				conn.id = atomic.AddUint64(&s.nextID, 1)
				conn.ctx = ctx

				if s.openHandler != nil {
					s.openHandler(conn)
				}

				s.serveConn(conn)
			}(req.Context())
		}
	}
}

func (s *Server) serveConn(c *Conn) {
	var closeErr error

loop:
	for {
		select {
		case fr := <-c.input:
			s.frHandler(c, fr)
		case err := <-c.errch:
			if err == nil {
				break loop
			}

			if ce, ok := err.(closeError); ok {
				closeErr = ce.err
				break loop
			}

			if ce, ok := err.(Error); ok {
				closeErr = ce
				break loop
			}

			if s.errHandler != nil {
				s.errHandler(c, err)
			}
		case <-c.closer:
			break loop
		}
	}

	if s.closeHandler != nil {
		s.closeHandler(c, closeErr)
	}

	c.c.Close()

	c.wg.Wait()
}

func (s *Server) handleFrame(c *Conn, fr *Frame) {
	// TODO: error if not masked
	if fr.IsMasked() {
		fr.Unmask()
	}

	if fr.IsControl() {
		s.handleControl(c, fr)
	} else {
		s.handleFrameData(c, fr)
	}
}

func (s *Server) handleFrameData(c *Conn, fr *Frame) {
	var data []byte

	isBinary := fr.Code() == CodeBinary

	bf := c.buffered
	if bf == nil {
		if fr.IsFin() {
			data = fr.Payload()
		} else {
			bf = bytebufferpool.Get()
			bf.Reset()

			c.buffered = bf
			bf.Write(fr.Payload())
		}
	} else {
		bf.Write(fr.Payload())
		if fr.IsFin() {
			data = bf.B
			c.buffered = nil
			defer bytebufferpool.Put(bf)
		}
	}

	if len(data) != 0 && s.msgHandler != nil {
		s.msgHandler(c, isBinary, data)
	}

	ReleaseFrame(fr)
}

func (s *Server) handleControl(c *Conn, fr *Frame) {
	switch {
	case fr.IsPing():
		s.handlePing(c, fr.Payload())
	case fr.IsPong():
		s.handlePong(c, fr.Payload())
	case fr.IsClose():
		s.handleClose(c, fr)
	}
}

func (s *Server) handlePing(c *Conn, data []byte) {
	if s.pingHandler != nil {
		s.pingHandler(c, data)
	}

	pong := AcquireFrame()
	pong.SetCode(CodePong)
	pong.SetPayload(data)
	pong.SetFin()

	c.WriteFrame(pong)
}

func (s *Server) handlePong(c *Conn, data []byte) {
	if s.pongHandler != nil {
		s.pongHandler(c, data)
	}
}

func (s *Server) handleClose(c *Conn, fr *Frame) {
	defer c.closeOnce.Do(func() { close(c.closer) })
	c.errch <- func() error {
		if fr.Status() != StatusNone {
			return Error{
				Status: fr.Status(),
				Reason: string(fr.Payload()),
			}
		}

		return nil
	}()

	status := fr.Status()

	fr = AcquireFrame()
	fr.SetClose()
	fr.SetStatus(status)
	fr.SetFin()

	// reply back
	c.WriteFrame(fr)
}
