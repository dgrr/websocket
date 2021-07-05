# websocket

WebSocket library for [fasthttp](https://github.com/valyala/fasthttp).

Checkout [examples](https://github.com/dgrr/websocket/blob/master/examples) to inspire yourself.

# Why another WebSocket package?

Other WebSocket packages don't allow concurrent Read/Write operations
and does not provide low level access to WebSocket packet crafting.

Following the fasthttp philosophy this library tries to take as much advantage
of the Golang's multi-threaded model as possible,
while keeping your code concurrently safe.

To see an example of what this package CAN do that others DONT checkout [the broadcast example](https://github.com/dgrr/websocket/blob/master/examples/broadcast.go).

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
