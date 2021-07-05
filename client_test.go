package websocket

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/valyala/fasthttp"
	"github.com/valyala/fasthttp/fasthttputil"
)

func BenchmarkRandKey(b *testing.B) {
	var bf []byte
	for i := 0; i < b.N; i++ {
		bf = makeRandKey(bf[:0])
	}
}

func TestDial(t *testing.T) {
	var text = []byte("Make fasthttp great again")
	var uri = "http://localhost:9843/"
	ln := fasthttputil.NewInmemoryListener()

	ws := Server{
		Origin: uri,
	}

	ws.HandleData(func(conn *Conn, isBinary bool, data []byte) {
		if !bytes.Equal(data, text) {
			panic(fmt.Sprintf("%s <> %s", data, text))
		}
	})

	s := fasthttp.Server{
		Handler: ws.Upgrade,
	}
	ch := make(chan struct{}, 1)
	go func() {
		s.Serve(ln)
		ch <- struct{}{}
	}()

	c, err := ln.Dial()
	if err != nil {
		t.Fatal(err)
	}

	conn, err := MakeClient(c, uri)
	if err != nil {
		t.Fatal(err)
	}

	_, err = conn.Write(text)
	if err != nil {
		t.Fatal(err)
	}

	conn.Close()
	ln.Close()

	select {
	case <-ch:
	case <-time.After(time.Second * 5):
		t.Fatal("timeout")
	}
}
