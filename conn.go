package websocket

import (
	"bufio"
	"context"
	"io"
	"net"
	"sync"
	"time"

	"github.com/valyala/bytebufferpool"
)

// Conn represents a WebSocket connection on the server side.
//
// This handler is compatible with io.Writer.
type Conn struct {
	c  net.Conn
	br *bufio.Reader
	bw *bufio.Writer

	input  chan *Frame
	output chan *Frame

	closer    chan struct{}
	closeOnce sync.Once

	errch chan error

	// buffered messages
	buffered *bytebufferpool.ByteBuffer

	id uint64

	// ReadTimeout ...
	ReadTimeout time.Duration

	// WriteTimeout ...
	WriteTimeout time.Duration

	// MaxPayloadSize prevents huge memory allocation.
	//
	// By default MaxPayloadSize is DefaultPayloadSize.
	MaxPayloadSize uint64

	wg sync.WaitGroup

	ctx context.Context
}

// ID returns a unique identifier for the connection.
func (c *Conn) ID() uint64 {
	return c.id
}

// UserValue returns the key associated value.
func (c *Conn) UserValue(key string) interface{} {
	return c.ctx.Value(key)
}

// SetUserValue assigns a key to the given value
func (c *Conn) SetUserValue(key string, value interface{}) {
	c.ctx = context.WithValue(c.ctx, key, value)
}

// LocalAddr returns local address.
func (c *Conn) LocalAddr() net.Addr {
	return c.c.LocalAddr()
}

// RemoteAddr returns peer remote address.
func (c *Conn) RemoteAddr() net.Addr {
	return c.c.RemoteAddr()
}

func acquireConn(c net.Conn) (conn *Conn) {
	conn = &Conn{}
	conn.reset(c)
	conn.wg.Add(2)

	go conn.readLoop()
	go conn.writeLoop()

	return conn
}

// DefaultPayloadSize defines the default payload size (when none was defined).
const DefaultPayloadSize = 1 << 20

// Reset resets conn values setting c as default connection endpoint.
func (c *Conn) reset(conn net.Conn) {
	c.input = make(chan *Frame, 128)
	c.output = make(chan *Frame, 128)
	c.closer = make(chan struct{}, 1)
	c.errch = make(chan error, 2)
	c.ReadTimeout = 0
	c.WriteTimeout = 0
	c.MaxPayloadSize = DefaultPayloadSize
	c.ctx = nil
	c.c = conn
	c.br = bufio.NewReader(conn)
	c.bw = bufio.NewWriter(conn)
}

func (c *Conn) readLoop() {
	defer c.wg.Done()

	for {
		fr := AcquireFrame()
		fr.SetPayloadSize(c.MaxPayloadSize)

		// if c.ReadTimeout != 0 {
		// }

		_, err := fr.ReadFrom(c.br)
		if err != nil {
			select {
			case c.errch <- closeError{err: err}:
			default:
			}

			ReleaseFrame(fr)

			break
		}

		isClose := fr.IsClose()

		c.input <- fr

		if isClose {
			break
		}
	}
}

type closeError struct {
	err error
}

func (ce closeError) Unwrap() error {
	return ce.err
}

func (ce closeError) Error() string {
	return ce.err.Error()
}

func (c *Conn) writeLoop() {
	defer c.wg.Done()

loop:
	for {
		select {
		case fr := <-c.output:
			if err := c.writeFrame(fr); err != nil {
				select {
				case c.errch <- closeError{err}:
				default:
				}
			}

			isClose := fr.IsClose()

			ReleaseFrame(fr)

			if isClose {
				return
			}
		case <-c.closer:
			break loop
		}
	}

	// flush all the frames
	for n := len(c.output); n >= 0; n-- {
		fr, ok := <-c.output
		if !ok {
			break
		}

		if err := c.writeFrame(fr); err != nil {
			break
		}
	}
}

func (c *Conn) writeFrame(fr *Frame) error {
	fr.SetPayloadSize(c.MaxPayloadSize)

	if c.WriteTimeout > 0 {
		c.c.SetWriteDeadline(time.Now().Add(c.WriteTimeout))
		defer c.c.SetWriteDeadline(time.Time{})
	}

	_, err := fr.WriteTo(c.bw)
	if err == nil {
		err = c.bw.Flush()
	}

	return err
}

func (c *Conn) Ping(data []byte) {
	fr := AcquireFrame()
	fr.SetPing()
	fr.SetFin()
	fr.SetPayload(data)

	c.WriteFrame(fr)
}

func (c *Conn) Write(data []byte) (int, error) {
	n := len(data)

	fr := AcquireFrame()

	fr.SetFin()
	fr.SetPayload(data)
	fr.SetText()

	c.WriteFrame(fr)

	return n, nil
}

func (c *Conn) WriteFrame(fr *Frame) {
	c.output <- fr
}

func (c *Conn) Close() error {
	c.CloseDetail(StatusNone, "")

	return nil
}

func (c *Conn) CloseDetail(status StatusCode, reason string) {
	if !c.isClosed() {
		fr := AcquireFrame()
		fr.SetClose()
		fr.SetStatus(status)
		fr.SetFin()

		io.WriteString(fr, reason)

		c.WriteFrame(fr)

		c.closeOnce.Do(func() { close(c.closer) })
	}

	return
}

func (c *Conn) isClosed() bool {
	select {
	case <-c.closer:
		return true
	default:
		// if we reach this point, that means `closer` is not closed
		// so we still have the connection alive
		return false
	}
}
