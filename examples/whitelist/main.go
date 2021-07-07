package main

import (
	"io"
	"log"
	"time"


	"github.com/dgrr/websocket"
	"github.com/valyala/fasthttp"
)

func main() {
	go startServer(":8080")

	startClient()
}

func startClient() {
	req := fasthttp.AcquireRequest()
	req.Header.DisableNormalizing()

	req.Header.Add("private-key", "123")

	log.Printf("Trying with key 123\n")

	c, err := websocket.DialWithHeaders("ws://localhost:8080/private_access", req)
	if err == nil {
		log.Fatalln("Should've failed")
	} else {
		log.Printf("Now trying with key 1234\n")

		req.Header.Set("private-key", "1234")
		c, err = websocket.DialWithHeaders("ws://localhost:8080/private_access", req)
	}

	if err != nil {
		log.Fatalln(err)
	}

	if _, err := io.WriteString(c, "Hello"); err != nil {
		panic(err)
	}

	fr := websocket.AcquireFrame()
	defer websocket.ReleaseFrame(fr)

	for i := 0; i < 5; i++ {
		fr.Reset()

		_, err := c.ReadFrame(fr)
		if err != nil {
			panic(err)
		}

		time.Sleep(time.Second)

		c.WriteFrame(fr)
	}

	c.Close()
}

func OnMessage(c *websocket.Conn, isBinary bool, data []byte) {
	log.Printf("Received: %s\n", data)
	c.Write(data)
}

func checkAccess(ctx *fasthttp.RequestCtx) bool {
	return string(ctx.Request.Header.Peek("private-key")) == "1234"
}

func startServer(addr string) {
	wServer := websocket.Server{
		UpgradeHandler: checkAccess,
	}

	wServer.HandleData(OnMessage)

	if err := fasthttp.ListenAndServe(addr, wServer.Upgrade); err != nil {
		log.Fatalln(err)
	}
}
