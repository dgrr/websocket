package main

import (
	"fmt"
	"net/http"

	"github.com/dgrr/websocket"
)

func OnMessage(c *websocket.Conn, isBinary bool, data []byte) {
	c.Write(data)
}

func main() {
	wS := websocket.Server{}
	wS.HandleData(OnMessage)

	s := http.Server{
		Addr: ":8081",
		Handler: http.HandlerFunc(wS.NetUpgrade),
	}

	fmt.Println(s.ListenAndServe())
}
