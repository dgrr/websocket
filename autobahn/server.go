package main

import (
	"github.com/dgrr/websocket"

	"github.com/valyala/fasthttp"
)

func main() {
	ws := websocket.Server{}

	ws.HandleData(wsHandler)

	fasthttp.ListenAndServe(":9000", ws.Upgrade)
}

func wsHandler(c *websocket.Conn, isBinary bool, data []byte) {
	c.Write(data)
}
