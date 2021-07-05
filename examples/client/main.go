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

	c, err := websocket.Dial("ws://localhost:8080/echo")
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

func OnMessage(c *websocket.ServerConn, isBinary bool, data []byte) {
	log.Printf("Received: %s\n", data)
	c.Write(data)
}

func startServer(addr string) {
	wServer := websocket.Server{}

	wServer.HandleData(OnMessage)

	if err := fasthttp.ListenAndServe(addr, wServer.Upgrade); err != nil {
		log.Fatalln(err)
	}
}
