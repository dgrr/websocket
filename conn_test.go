package websocket

import (
	"bufio"
	"fmt"
	"github.com/valyala/fasthttp"
	"github.com/valyala/fasthttp/fasthttputil"
	"io"
	"testing"
)

func configureServer(t *testing.T) (*fasthttp.Server, *fasthttputil.InmemoryListener) {
	ln := fasthttputil.NewInmemoryListener()

	ws := Server{}

	var stage = 0

	ws.HandleData(func(c *Conn, isBinary bool, data []byte) {
		switch stage {
		case 0:
			if isBinary {
				t.Fatal("Unexpected binary mode")
			}

			if string(data) != "Hello" {
				t.Fatalf("Expecting Hello, got %s", data)
			}

			io.WriteString(c, "Hello2")
		case 2:
			if string(data) != "Hello world" {
				t.Fatalf("Expecting Hello world, got %s", data)
			}

			c.CloseDetail(StatusGoAway, "Bye")
		}

		stage++
	})

	ws.HandlePing(func(c *Conn, data []byte) {
		if string(data) != "content" {
			t.Fatalf("Expecting content, got %s", data)
		}

		stage++
	})

	ws.HandleClose(func(c *Conn, err error) {
		if err != nil {
			t.Fatal(err)
		}
	})

	s := &fasthttp.Server{
		Handler: ws.Upgrade,
	}

	return s, ln
}

func openConn(t *testing.T, ln *fasthttputil.InmemoryListener) *Client {
	c, err := ln.Dial()
	if err != nil {
		t.Fatal(err)
	}

	// TODO: UpgradeAsClient
	fmt.Fprintf(c, "GET / HTTP/1.1\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Version: 13\r\n\r\n")

	br := bufio.NewReader(c)
	var res fasthttp.Response
	err = res.Read(br)
	if err != nil {
		t.Fatal(err)
	}

	conn := &Client{
		c: c,
		brw: bufio.NewReadWriter(
			bufio.NewReader(c), bufio.NewWriter(c)),
	}

	return conn
}

func TestReadFrame(t *testing.T) {
	s, ln := configureServer(t)
	ch := make(chan struct{})
	go func() {
		s.Serve(ln)
		ch <- struct{}{}
	}()

	conn := openConn(t, ln)
	io.WriteString(conn, "Hello")

	fr := AcquireFrame()

	_, err := conn.ReadFrame(fr)
	if err != nil {
		t.Fatal(err)
	}

	b := fr.Payload()
	if string(b) != "Hello2" {
		t.Fatalf("Unexpected message: %s<>Hello2", b)
	}

	fr.Reset()
	fr.SetPing()
	fr.SetFin()
	fr.SetPayload([]byte("content"))

	conn.WriteFrame(fr)

	_, err = conn.ReadFrame(fr)
	if err != nil {
		t.Fatal(err)
	}
	if !fr.IsPong() {
		t.Fatal("Unexpected message: no pong")
	}
	fr.Reset()

	fr.SetText()
	fr.SetPayload([]byte("Hello"))
	fr.Mask()
	_, err = conn.WriteFrame(fr)
	if err != nil {
		t.Fatal(err)
	}
	fr.Reset()

	fr.SetContinuation()
	fr.SetFin()
	fr.SetPayload([]byte(" world"))
	fr.Mask()
	_, err = conn.WriteFrame(fr)
	if err != nil {
		t.Fatal(err)
	}

	_, err = conn.ReadFrame(fr)
	if err != nil {
		t.Fatal(err)
	}
	if !fr.IsClose() {
		t.Fatal("Unexpected frame: no close")
	}
	p := fr.Payload()
	if string(p) != "Bye" {
		t.Fatalf("Unexpected payload: %v <> Bye", p)
	}
	if fr.Status() != StatusGoAway {
		t.Fatalf("Status unexpected: %d <> %d", fr.Status(), StatusGoAway)
	}

	_, err = conn.WriteFrame(fr)
	if err != nil {
		t.Fatal(err)
	}

	ln.Close()
	<-ch
}

// func TestUserValue(t *testing.T) {
// 	var uri = "http://localhost:9843/"
// 	var text = "Hello user!!"
// 	ln := fasthttputil.NewInmemoryListener()
// 	upgr := Upgrader{
// 		Origin: uri,
// 		Handler: func(conn *Conn) {
// 			v := conn.UserValue("custom")
// 			if v == nil {
// 				t.Fatal("custom is nil")
// 			}
// 			conn.WriteString(v.(string))
// 		},
// 	}
// 	s := fasthttp.Server{
// 		Handler: func(ctx *fasthttp.RequestCtx) {
// 			ctx.SetUserValue("custom", text)
// 			upgr.Upgrade(ctx)
// 		},
// 	}
// 	ch := make(chan struct{}, 1)
// 	go func() {
// 		s.Serve(ln)
// 		ch <- struct{}{}
// 	}()
//
// 	c, err := ln.Dial()
// 	if err != nil {
// 		t.Fatal(err)
// 	}
//
// 	conn, err := Client(c, uri)
// 	if err != nil {
// 		t.Fatal(err)
// 	}
//
// 	_, b, err := conn.ReadMessage(nil)
// 	if err != nil {
// 		t.Fatal(err)
// 	}
//
// 	if string(b) != text {
// 		t.Fatalf("client expecting %s. Got %s", text, b)
// 	}
//
// 	conn.Close()
// 	ln.Close()
//
// 	select {
// 	case <-ch:
// 	case <-time.After(time.Second * 5):
// 		t.Fatal("timeout")
// 	}
// }
