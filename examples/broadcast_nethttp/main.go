package main

import (
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/dgrr/websocket"
)

type Broadcaster struct {
	cs  sync.Map
}

func (b *Broadcaster) OnOpen(c *websocket.Conn) {
	b.cs.Store(c.ID(), c)

	log.Printf("%s connected\n", c.RemoteAddr())
}

func (b *Broadcaster) OnClose(c *websocket.Conn, err error) {
	if err != nil {
		log.Printf("%d closed with error: %s\n", c.ID(), err)
	} else {
		log.Printf("%d closed the connection\n", c.ID())
	}

	b.cs.Delete(c.ID())
}

func (b *Broadcaster) Start(i int) {
	time.AfterFunc(time.Second, b.sendData(i))
}

func (b *Broadcaster) sendData(i int) func() {
	return func() {
		b.cs.Range(func(_, v interface{}) bool {
			nc := v.(*websocket.Conn)
			fmt.Fprintf(nc, "Sending message number %d\n", i)

			return true
		})

		b.Start(i+1)
	}
}

func main() {
	b := &Broadcaster{}

	wServer := websocket.Server{}
	wServer.HandleOpen(b.OnOpen)
	wServer.HandleClose(b.OnClose)

	http.HandleFunc("/", rootHandler)
	http.HandleFunc("/ws", wServer.NetUpgrade)

	b.Start(0)

	http.ListenAndServe(":8080", nil)
}

func rootHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintln(w, `<!DOCTYPE html>
<html>
  <head>
    <meta charset="UTF-8"/>
    <title>Sample of websocket with Golang</title>
  </head>
  <body>
		<div id="text"></div>
    <script>
      var ws = new WebSocket("ws://localhost:8080/ws");
      ws.onmessage = function(e) {
				var d = document.createElement("div");
        d.innerHTML = e.data;
				ws.send(e.data);
        document.getElementById("text").appendChild(d);
      }
			ws.onclose = function(e){
				var d = document.createElement("div");
				d.innerHTML = "CLOSED";
        document.getElementById("text").appendChild(d);
			}
    </script>
  </body>
</html>`)
}
