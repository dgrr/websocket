package websocket

import (
	"bufio"
	"github.com/valyala/bytebufferpool"
	"net"
	"sync"
	"time"
)

// ServerConn represents websocket connection handler.
//
// This handler is compatible with io.Reader, io.ReaderFrom, io.Writer, io.WriterTo
type ServerConn struct {
	c  net.Conn
	bw *bufio.Writer

	input  chan *Frame
	output chan *Frame
	closer chan struct{}
	errch  chan error

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

	userValues map[string]interface{}
}

// ID returns a unique identifier for the connection.
func (c *ServerConn) ID() uint64 {
	return c.id
}

// UserValue returns the key associated value.
func (c *ServerConn) UserValue(key string) interface{} {
	return c.userValues[key]
}

// SetUserValue assigns a key to the given value
func (c *ServerConn) SetUserValue(key string, value interface{}) {
	c.userValues[key] = value
}

// LocalAddr returns local address.
func (c *ServerConn) LocalAddr() net.Addr {
	return c.c.LocalAddr()
}

// RemoteAddr returns peer remote address.
func (c *ServerConn) RemoteAddr() net.Addr {
	return c.c.RemoteAddr()
}

func acquireConn(c net.Conn) (conn *ServerConn) {
	conn = &ServerConn{}
	conn.reset(c)
	conn.wg.Add(2)

	go conn.readLoop()
	go conn.writeLoop()

	return conn
}

// DefaultPayloadSize defines the default payload size (when none was defined).
const DefaultPayloadSize = 1 << 20

// Reset resets conn values setting c as default connection endpoint.
func (c *ServerConn) reset(conn net.Conn) {
	c.input = make(chan *Frame, 128)
	c.output = make(chan *Frame, 128)
	c.closer = make(chan struct{}, 1)
	c.errch = make(chan error, 2)
	c.ReadTimeout = 0
	c.WriteTimeout = 0
	c.MaxPayloadSize = DefaultPayloadSize
	c.userValues = make(map[string]interface{})
	c.c = conn
	c.bw = bufio.NewWriter(conn)
}

func (c *ServerConn) readLoop() {
	defer c.wg.Done()
	defer c.Close()

	for {
		fr := AcquireFrame()
		fr.SetPayloadSize(c.MaxPayloadSize)

		// if c.ReadTimeout != 0 {
		// }

		_, err := fr.ReadFrom(c.c)
		if err != nil {
			select {
			case c.errch <- closeError{err: err}:
			default:
			}

			ReleaseFrame(fr)

			break
		}

		c.input <- fr

		if fr.IsClose() {
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

func (c *ServerConn) writeLoop() {
	defer c.wg.Done()

	for {
		select {
		case fr := <-c.output:
			if err := c.writeFrame(fr); err != nil {
				select {
				case c.errch <- closeError{err}:
				default:
				}
			}

			ReleaseFrame(fr)
		case <-c.closer:
			return
		}
	}
}

func (c *ServerConn) writeFrame(fr *Frame) error {
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

func (c *ServerConn) Write(data []byte) (int, error) {
	n := len(data)

	fr := AcquireFrame()

	fr.SetFin()
	fr.SetPayload(data)
	fr.SetText()

	c.WriteFrame(fr)

	return n, nil
}

func (c *ServerConn) WriteFrame(fr *Frame) {
	c.output <- fr
}

func (c *ServerConn) Close() error {
	select {
	case <-c.closer:
	default:
		return nil
	}

	close(c.closer)

	return c.c.Close()
}
