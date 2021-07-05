package websocket

import (
	"bytes"
	"net"
	"sync"
	"sync/atomic"

	"github.com/valyala/bytebufferpool"
	"github.com/valyala/fasthttp"
)

type (
	// OpenHandler ...
	OpenHandler    func(c *ServerConn)
	PingHandler    func(c *ServerConn, data []byte)
	PongHandler    func(c *ServerConn, data []byte)
	MessageHandler func(c *ServerConn, isBinary bool, data []byte)
	FrameHandler   func(c *ServerConn, fr *Frame)
	CloseHandler   func(c *ServerConn, err error)
	ErrorHandler   func(c *ServerConn, err error)
)

// Server represents the WebSocket server.
//
// Server is going to be in charge of upgrading the connection, is not a server per-se.
type Server struct {
	// UpgradeHandler allows user to handle RequestCtx when upgrading.
	//
	// If UpgradeHandler returns false the connection won't be upgraded and
	// the parsed ctx will be used as a response.
	UpgradeHandler UpgradeHandler

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

	onceEnsure sync.Once
}

func (s *Server) checkDefaults() {
	if s.frHandler != nil {
		return
	}

	if s.pingHandler == nil {
		s.pingHandler = s.handlePing
	}

	s.frHandler = s.handleFrame
}

// HandleData sets the MessageHandler.
func (s *Server) HandleData(msgHandler MessageHandler) {
	s.msgHandler = msgHandler
}

func (s *Server) HandleOpen(openHandler OpenHandler) {
	s.openHandler = openHandler
}

func (s *Server) HandleClose(closeHandler CloseHandler) {
	s.closeHandler = closeHandler
}

func (s *Server) HandlePing(pingHandler PingHandler) {
	s.pingHandler = pingHandler
}

func (s *Server) HandlePong(pongHandler PongHandler) {
	s.pongHandler = pongHandler
}

func (s *Server) HandleError(errHandler ErrorHandler) {
	s.errHandler = errHandler
}

func (s *Server) HandleFrame(frameHandler FrameHandler) {
	s.frHandler = frameHandler
}

func (s *Server) Upgrade(ctx *fasthttp.RequestCtx) {
	if !ctx.IsGet() {
		ctx.SetStatusCode(fasthttp.StatusBadRequest)
		return
	}

	s.onceEnsure.Do(s.checkDefaults)

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
			//compress := mustCompress(exts)

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

			userValues := make(map[string]interface{})
			ctx.VisitUserValues(func(k []byte, v interface{}) {
				userValues[string(k)] = v
			})

			ctx.Hijack(func(c net.Conn) {
				conn := acquireConn(c)
				conn.id = atomic.AddUint64(&s.nextID, 1)
				// establishing default options
				conn.userValues = userValues

				s.serveConn(conn)
			})
		}
	}
}

func (s *Server) serveConn(c *ServerConn) {
	if s.openHandler != nil {
		s.openHandler(c)
	}

	var closeErr error

loop:
	for {
		select {
		case fr := <-c.input:
			s.frHandler(c, fr)
		case err := <-c.errch:
			if ce, ok := err.(closeError); ok {
				closeErr = ce.err
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

func (s *Server) handleFrame(c *ServerConn, fr *Frame) {
	if fr.IsControl() {
		s.handleControl(c, fr)
	} else {
		s.handleFrameData(c, fr)
	}
}

func (s *Server) handleFrameData(c *ServerConn, fr *Frame) {
	var data []byte

	isBinary := fr.Code() == CodeBinary

	// TODO: error if not masked
	if fr.IsMasked() {
		fr.Unmask()
	}

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

func (s *Server) handleControl(c *ServerConn, fr *Frame) {
	switch {
	case fr.IsPing():
		s.pingHandler(c, fr.Payload())
	case fr.IsPong():
		s.pongHandler(c, fr.Payload())
	case fr.IsClose():
		s.handleClose(c, fr)
	}
}

func (s *Server) handlePing(c *ServerConn, data []byte) {
	pong := AcquireFrame()
	pong.SetCode(CodePong)
	pong.SetPayload(data)

	c.WriteFrame(pong)
}

func (s *Server) handlePong(c *ServerConn, data []byte) {

}

func (s *Server) handleClose(c *ServerConn, fr *Frame) {
	fr.Unmask()

	var err = func() error {
		if fr.Status() != StatusNone {
			return Error{
				Status: fr.Status(),
				Reason: string(fr.Payload()),
			}
		}

		return nil
	}()

	if s.closeHandler != nil {
		s.closeHandler(c, err)
	}

	frRes := AcquireFrame()
	frRes.SetClose()
	frRes.SetStatus(fr.Status())

	c.writeFrame(frRes)
}
