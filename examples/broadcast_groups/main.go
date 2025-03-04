package main

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/dgrr/websocket"
	"github.com/fasthttp/router"
	"github.com/valyala/fasthttp"
)

type Broadcaster struct {
	cs sync.Map
}

func (b *Broadcaster) Broadcast(i int) *Broadcaster {
	if b == nil {
		return nil
	}
	b.cs.Range(func(_, v interface{}) bool {
		nc := v.(*websocket.Conn)
		fmt.Fprintf(nc, "Sending message number %d\n", i)

		return true
	})
	return b
}

type BroadcasterGroup struct {
	broadcasters              sync.Map
	broadcastersByConnections sync.Map
}

func (g *BroadcasterGroup) GetBroadcaster(id interface{}) *Broadcaster {
	b, ok := g.broadcasters.Load(id)
	if !ok {
		return nil
	}
	return b.(*Broadcaster)
}

func (g *BroadcasterGroup) Join(id interface{}, c *websocket.Conn) *Broadcaster {
	bb, _ := g.broadcasters.LoadOrStore(id, &Broadcaster{})
	b := bb.(*Broadcaster)
	b.cs.Store(c.ID(), c)

	mm, _ := g.broadcastersByConnections.LoadOrStore(c.ID(), &sync.Map{})
	m := mm.(*sync.Map)
	m.Store(id, true)

	return b
}

func (g *BroadcasterGroup) Leave(id interface{}, c *websocket.Conn) {
	b := g.GetBroadcaster(id)
	if b == nil {
		return
	}

	mm, ok := g.broadcastersByConnections.Load(c.ID())
	if ok {
		m := mm.(*sync.Map)
		m.Delete(id)
	}

	b.cs.Delete(c.ID())

	isEmpty := true
	b.cs.Range(func(key, value interface{}) bool {
		isEmpty = false
		return false
	})
	if isEmpty {
		bb, loaded := g.broadcasters.LoadAndDelete(id)
		if loaded {
			bb.(*Broadcaster).cs.Range(func(key, value interface{}) bool {
				g.Join(id, value.(*websocket.Conn))
				return true
			})
		}
	}
}

func (g *BroadcasterGroup) LeaveAll(c *websocket.Conn) {
	mm, loaded := g.broadcastersByConnections.LoadAndDelete(c.ID())
	if !loaded {
		return
	}
	m := mm.(*sync.Map)
	m.Range(func(key, value interface{}) bool {
		g.Leave(key, c)
		return true
	})
}

var group = &BroadcasterGroup{}

func OnOpen(c *websocket.Conn) {
	roomID := c.ID() % 3
	group.Join(roomID, c)

	log.Printf("%s joined room %d\n", c.RemoteAddr(), roomID)
}

func OnClose(c *websocket.Conn, err error) {
	if err != nil {
		log.Printf("%d closed with error: %s\n", c.ID(), err)
	} else {
		log.Printf("%d closed the connection\n", c.ID())
	}

	group.LeaveAll(c)
}

func (g *BroadcasterGroup) Start(i int) {
	time.AfterFunc(time.Second, g.sendData(i))
}

func (g *BroadcasterGroup) sendData(i int) func() {
	return func() {
		g.broadcasters.Range(func(k, v interface{}) bool {
			r := k.(uint64)
			if r == uint64(i%3) {
				v.(*Broadcaster).Broadcast(i)
				log.Printf("Sending message number %d to room %d\n", i, k.(uint64))
			}

			return true
		})

		g.Start(i + 1)
	}
}

func main() {
	wServer := websocket.Server{}
	wServer.HandleOpen(OnOpen)
	wServer.HandleClose(OnClose)

	router := router.New()
	router.GET("/", rootHandler)
	router.GET("/ws", wServer.Upgrade)

	server := fasthttp.Server{
		Handler: router.Handler,
	}

	group.Start(0)

	server.ListenAndServe(":8080")
}

func rootHandler(ctx *fasthttp.RequestCtx) {
	ctx.SetContentType("text/html")
	fmt.Fprintln(ctx, `<!DOCTYPE html>
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
