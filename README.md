# websocket

WebSocket library for [fasthttp](https://github.com/valyala/fasthttp).

Checkout [examples](https://github.com/dgrr/websocket/blob/master/examples) to inspire yourself.

# Why another WebSocket package?

Other WebSocket packages don't allow concurrent Read/Write operations on servers
and they do not provide low level access to WebSocket packet crafting.
Those WebSocket packages try to emulate the Golang API by implementing
io.Reader and io.Writer interfaces on their connections. io.Writer might be a
good idea to use it, but not io.Reader, given that WebSocket is an async protocol
by nature (all protocols are (?)).

Sometimes, WebSocket servers are just cumbersome when we want to handle a lot of
clients in an async manner. For example, in other WebSocket packages to broadcast
a message generated internally we'll need to do the following:

```go
type MyWebSocketService struct {
    clients sync.Map
}

type BlockingConn struct {
	lck sync.Mutex
	c websocketPackage.Conn
}

func (ws *MyWebSocketService) Broadcaster() {
	for msg := range messageProducerChannel {
        ws.clients.Range(func(_, v interface{}) bool {
            c := v.(*BlockingConn)
            c.lck.Lock() // oh, we need to block, otherwise we can break the program
            err := c.Write(msg)
            c.lck.Unlock()
            
            if err != nil {
                // we have an error, what can we do? Log it?
            	// if the connection has been closed we'll receive that on
            	// the Read call, so the connection will close automatically.
            }
            
            return true
        })
    }
}

func (ws *MyWebSocketService) Handle(request, response) {
	c, err := websocketPackage.Upgrade(request, response)
	if err != nil {
		// then it's clearly an error! Report back
    }
    
    bc := &BlockingConn{
        c: c,
    }   
    
	ws.clients.Store(bc, struct{}{})
    
	// even though I just want to write, I need to block somehow
    for {
    	content, err := bc.Read()
    	if err != nil {
            // handle the error
            break
        }
    }
    
    ws.clients.Delete(bc)
}
```

First, we need to store every client upon connection,
and whenever we want to send data we need to iterate over a list, and send the message.
If while, writing we get an error, then we need to handle that client's error
What if the writing operation is happening at the same time in 2 different coroutines?
Then we need a sync.Mutex and block until we finish writing.

To solve most of those problems [websocket](https://github.com/dgrr/websocket)
uses channels and separated coroutines, one for reading and another one for writing.
By following the [sharing principle](https://golang.org/doc/effective_go#sharing).

> Do not communicate by sharing memory; instead, share memory by communicating.

Following the fasthttp philosophy this library tries to take as much advantage
of the Golang's multi-threaded model as possible,
while keeping your code concurrently safe.

To see an example of what this package CAN do that others DONT checkout [the broadcast example](https://github.com/dgrr/websocket/blob/master/examples/broadcast/main.go).

# Server

## How can I launch a server?

It's quite easy. You only need to create a [Server](https://pkg.go.dev/github.com/dgrr/websocket?utm_source=godoc#Server),
set your callbacks by calling the [Handle*](https://pkg.go.dev/github.com/dgrr/websocket?utm_source=godoc#Server.HandleClose) methods
and then specify your fasthttp handler as [Server.Upgrade](https://pkg.go.dev/github.com/dgrr/websocket?utm_source=godoc#Server.Upgrade).

```go
package main

import (
	"fmt"
	
	"github.com/valyala/fasthttp"
	"github.com/dgrr/websocket"
)

func main() {
	ws := websocket.Server{}
	ws.HandleData(OnMessage)
	
	fasthttp.ListenAndServe(":8080", ws.Upgrade)
}

func OnMessage(c *websocket.Conn, isBinary bool, data []byte) {
	fmt.Printf("Received data from %s: %s\n", c.RemoteAddr(), data)
}
```

## How can I handle pings?

Pings are handle automatically by the library, but you can get the content of
those pings setting the callback using [HandlePing](https://pkg.go.dev/github.com/dgrr/websocket?utm_source=godoc#Server.HandlePing).

For example, let's try to get the round trip time to a client by using
the PING frame.

```go
package main

import (
	"sync"
	"encoding/binary"
	"log"
	"time"

	"github.com/valyala/fasthttp"
	"github.com/dgrr/websocket"
)

// Struct to keep the clients connected
//
// it should be safe to access the clients concurrently from Open and Close.
type RTTMeasure struct {
	clients sync.Map
}

// just trigger the ping sender
func (rtt *RTTMeasure) Start() {
	time.AfterFunc(time.Second * 2, rtt.sendPings)
}

func (rtt *RTTMeasure) sendPings() {
	var data [8]byte

	binary.BigEndian.PutUint64(data[:], uint64(
		time.Now().UnixNano()),
	)

	rtt.clients.Range(func(_, v interface{}) bool {
		c := v.(*websocket.Conn)
		c.Ping(data[:])
		return true
	})

	rtt.Start()
}

// register a connection when it's open
func (rtt *RTTMeasure) RegisterConn(c *websocket.Conn) {
	rtt.clients.Store(c.ID(), c)
	log.Printf("Client %s connected\n", c.RemoteAddr())
}

// remove the connection when receiving the close
func (rtt *RTTMeasure) RemoveConn(c *websocket.Conn, err error) {
	rtt.clients.Delete(c.ID())
	log.Printf("Client %s disconnected\n", c.RemoteAddr())
}

func main() {
	rtt := RTTMeasure{}

	ws := websocket.Server{}
	ws.HandleOpen(rtt.RegisterConn)
	ws.HandleClose(rtt.RemoveConn)
	ws.HandlePong(OnPong)

	// schedule the timer
	rtt.Start()

	fasthttp.ListenAndServe(":8080", ws.Upgrade)
}

// handle the pong message
func OnPong(c *websocket.Conn, data []byte) {
	if len(data) == 8 {
		n := binary.BigEndian.Uint64(data)
		ts := time.Unix(0, int64(n))

		log.Printf("RTT with %s is %s\n", c.RemoteAddr(), time.Now().Sub(ts))
	}
}
```

# websocket vs gorilla vs nhooyr vs gobwas

| Features | [websocket](https://github.com/dgrr/websocket) | [Gorilla](https://github.com/fasthttp/websocket)| [Nhooyr](https://github.com/nhooyr/websocket) | [gowabs](https://github.com/gobwas/ws) |
| --- | --- | --- | --- | --- |
| Concurrent R/W                          | Yes            | No           | No. Only writes | No           |
| Passes Autobahn Test Suite              | Mostly         | Yes          | Yes             | Mostly       |    
| Receive fragmented message              | Yes            | Yes          | Yes             | Yes          |
| Send close message                      | Yes            | Yes          | Yes             | Yes          |
| Send pings and receive pongs            | Yes            | Yes          | Yes             | Yes          |
| Get the type of a received data message | Yes            | Yes          | Yes             | Yes          |
| Compression Extensions                  | No             | Experimental | Yes             | No (?)       |
| Read message using io.Reader            | No             | Yes          | No              | No (?)       |
| Write message using io.WriteCloser      | No             | Yes          | No              | No (?)       |

# Stress tests

The following stress test were performed without timeouts:

Executing `tcpkali --ws -c 100 -m 'hello world!!13212312!' -r 10k localhost:8081` the tests shows the following:

Websocket:
```
Total data sent:     267.7 MiB (280678466 bytes)
Total data received: 229.5 MiB (240626600 bytes)
Bandwidth per channel: 4.167⇅ Mbps (520.9 kBps)
Aggregate bandwidth: 192.357↓, 224.375↑ Mbps
Packet rate estimate: 247050.1↓, 61842.9↑ (1↓, 1↑ TCP MSS/op)
Test duration: 10.0075 s.
```

Gorilla:
```
Total data sent:     260.2 MiB (272886768 bytes)
Total data received: 109.3 MiB (114632982 bytes)
Bandwidth per channel: 3.097⇅ Mbps (387.1 kBps)
Aggregate bandwidth: 91.615↓, 218.092↑ Mbps
Packet rate estimate: 109755.3↓, 66807.4↑ (1↓, 1↑ TCP MSS/op)
Test duration: 10.01 s.
```

Nhooyr: (Don't know why is that low)
```
Total data sent:     224.3 MiB (235184096 bytes)
Total data received: 41.2 MiB (43209780 bytes)
Bandwidth per channel: 2.227⇅ Mbps (278.3 kBps)
Aggregate bandwidth: 34.559↓, 188.097↑ Mbps
Packet rate estimate: 88474.0↓, 55256.1↑ (1↓, 1↑ TCP MSS/op)
Test duration: 10.0027 s.
```

Gobwas:
```
Total data sent:     265.8 MiB (278718160 bytes)
Total data received: 117.8 MiB (123548959 bytes)
Bandwidth per channel: 3.218⇅ Mbps (402.2 kBps)
Aggregate bandwidth: 98.825↓, 222.942↑ Mbps
Packet rate estimate: 148231.6↓, 72106.1↑ (1↓, 1↑ TCP MSS/op)
Test duration: 10.0015 s.
```

The source files are in [this](https://github.com/dgrr/websocket/tree/master/stress-tests/) folder.
