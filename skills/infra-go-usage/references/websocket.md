# websocket

A WebSocket server wrapper built on [gorilla/websocket](https://github.com/gorilla/websocket),
providing connection management, event dispatch, room broadcasting, and heartbeat checks.

## Features

- **Event-driven**: `EventHandler` dispatches JSON `{type, data}` messages to registered handlers
- **Room system**: in-memory + Redis backends, supporting `Join/Leave` and per-room broadcast
- **Cluster support**: cross-instance broadcast through Redis Pub/Sub, with globally unique conn IDs
- **Fluent broadcasting**: `srv.To("room1", "room2").PushText("hello")` targets rooms in one line
- **Heartbeat**: automatic Ping/Pong heartbeats that disconnect on timeout
- **Thread safety**: every write is protected by a mutex, so concurrent writes are supported
- **User value storage**: `conn.Set("userID", "xxx")` passes context between Handlers
- **Config-driven**: Config defines defaults via `default` struct tags, following the conf standard
- **Consistent style**: English comments, English error messages, and functional options, in line
  with the other infra-go modules

## Installation

```bash
go get github.com/chihqiang/infra-go/websocket
```

## Quick start

### Basic example

```go
package main

import (
    "encoding/json"
    "net/http"
    "time"

    "github.com/chihqiang/infra-go/logger"
    "github.com/chihqiang/infra-go/websocket"
)

func main() {
    // create the event-driven handler
    handler := websocket.NewEventHandler()

    // connection established
    handler.OnOpen(func(conn *websocket.Conn) {
        logger.Infof("connection opened: %d", conn.ID())
        conn.Set("joinedAt", time.Now())
    })

    // event handling: chat message
    handler.Handle("chat", func(conn *websocket.Conn, data json.RawMessage) {
        var msg struct {
            Text string `json:"text"`
        }
        _ = json.Unmarshal(data, &msg)

        // broadcast to all connections
        conn.Server().BroadcastEvent("message", map[string]string{
            "text":     msg.Text,
            "sender":   "anonymous",
        })
    })

    // event handling: join a room
    handler.Handle("join", func(conn *websocket.Conn, data json.RawMessage) {
        var room string
        _ = json.Unmarshal(data, &room)
        conn.Join(room)
        conn.Emit("joined", room)
    })

    // connection closed
    handler.OnClose(func(conn *websocket.Conn, err error) {
        logger.Infof("connection closed: %d", conn.ID())
    })

    // create the server
    srv := websocket.MustNew(websocket.Config{}, handler)
    defer srv.Close()

    http.Handle("/ws", srv)
    logger.Fatal("server failed", logger.Err(http.ListenAndServe(":8080", nil)))
}
```

### Redis rooms (multi-instance deployment)

In a cluster every instance sets a different `NodeID` and connection IDs are encoded as
`nodeID<<32 | localCounter`, which keeps them globally unique. Cross-instance broadcasting happens
automatically through Redis Pub/Sub:

```go
package main

import (
    "encoding/json"
    "net/http"

    "github.com/chihqiang/infra-go/websocket"
    "github.com/redis/go-redis/v9"
)

func main() {
    rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})

    handler := websocket.NewEventHandler()

    handler.Handle("join", func(conn *websocket.Conn, data json.RawMessage) {
        var room string
        _ = json.Unmarshal(data, &room)
        conn.Join(room)
    })

    handler.Handle("chat", func(conn *websocket.Conn, data json.RawMessage) {
        var msg struct{ Text string `json:"text"` }
        _ = json.Unmarshal(data, &msg)
        // broadcast to a specific room (cross-instance automatically)
        conn.Server().To("lobby").Emit("message", msg)
    })

    // Redis rooms + NodeID: several instances share room state and broadcasts
    srv := websocket.MustNew(websocket.Config{
        RoomType: "redis",
        NodeID:   1, // every instance sets a different NodeID
    }, handler, websocket.WithRedisClient(rdb))
    defer srv.Close()

    http.Handle("/ws", srv)
    http.ListenAndServe(":8080", nil)
}
```

### Custom Handler

Implementing the `Handler` interface gives full control over the connection lifecycle:

```go
type myHandler struct{}

func (h *myHandler) HandleOpen(conn *websocket.Conn) {
    logger.Infof("connection opened: %d", conn.ID())
}

func (h *myHandler) HandleMessage(conn *websocket.Conn, messageType int, data []byte) {
    // raw message handling (not event-driven)
    _ = conn.WriteMessage(messageType, data) // echo
}

func (h *myHandler) HandleClose(conn *websocket.Conn, err error) {
    logger.Infof("connection closed: %d", conn.ID())
}

func (h *myHandler) HandleError(conn *websocket.Conn, err error) {
    logger.Error("connection error", logger.Int("conn_id", conn.ID()), logger.Err(err))
}

func main() {
    srv := websocket.MustNew(websocket.Config{}, &myHandler{})
    http.Handle("/ws", srv)
    http.ListenAndServe(":8080", nil)
}
```

## API

### Creating a server

```go
// create a server (returns an error)
srv, err := websocket.New(websocket.Config{}, handler)

// create a server (panics on error, suited to global initialization)
srv := websocket.MustNew(websocket.Config{}, handler)

// with options
srv := websocket.MustNew(websocket.Config{}, handler,
    websocket.WithLogger(myLogger),
    websocket.WithCheckOrigin(func(r *http.Request) bool {
        return r.Host == "example.com"
    }),
)
```

### Options

| Option | Description |
| ------ | ------ |
| `WithLogger(l logger.ILogger)` | Set the logger; defaults to `logger.GetGlobal()` |
| `WithRedisClient(client)` | Set the Redis client (used for Redis rooms) |
| `WithPubSub(ps)` | Set a custom PubSub implementation (used for cluster broadcast) |
| `WithCheckOrigin(fn)` | Set the Origin check function; allows every origin by default |
| `WithSubprotocols(protocols...)` | Set the subprotocol negotiation list |

### Server methods

```go
// connection management
conn, ok := srv.GetConn(id)     // get a connection by its ID
count := srv.Count()            // number of live connections
err := srv.CloseConn(id)        // close a specific connection
err := srv.Close()              // close the server (disconnecting every connection)

// broadcast
srv.Broadcast(data)            // broadcast a text message to all connections
srv.BroadcastText("hello")      // broadcast a string to all connections
srv.BroadcastJSON(v)           // broadcast JSON to all connections
srv.BroadcastEvent("news", v)   // broadcast an event to all connections

// targeted broadcast (returns a Broadcaster)
srv.To("room1", "room2").PushText("hello")
srv.To("room1").Emit("event", data)
srv.To("room1").WriteJSON(v)

// room management
room := srv.Room()              // get the room manager
clients := room.GetClients("room1") // list the connection IDs inside a room
```

### Conn methods

```go
// connection info
id := conn.ID()                 // unique connection ID
srv := conn.Server()            // the owning server
addr := conn.RemoteAddr()       // client address
req := conn.Request()           // the original HTTP request (read-only)

// sending messages
conn.WriteText(data)            // send a text message
conn.WriteTextString("hello")   // send a string text message
conn.WriteBinary(data)          // send a binary message
conn.WriteJSON(v)               // send a JSON message
conn.WriteMessage(type, data)    // send a message of the given type
conn.Emit("event", data)        // send an event {"type":"event","data":...}

// room operations
conn.Join("room1", "room2")    // join rooms
conn.Leave("room1")              // leave a room
rooms := conn.Rooms()           // all rooms the connection is currently in

// user values
conn.Set("userID", "user-123")  // set a user value
val, ok := conn.Get("userID")   // get a user value
val := conn.MustGet("userID")   // get a user value (nil when absent)

// close
conn.Close()                    // close the connection
conn.IsClosed()                 // whether the connection is already closed
```

### EventHandler

`EventHandler` is the default `Handler` implementation and supports event-driven message handling:

```go
h := websocket.NewEventHandler()

// register callbacks in a chain
h.OnOpen(func(conn *websocket.Conn) {
    logger.Infof("connection opened: %d", conn.ID())
}).OnClose(func(conn *websocket.Conn, err error) {
    logger.Infof("connection closed: %d", conn.ID())
}).OnError(func(conn *websocket.Conn, err error) {
    logger.Error("websocket error", logger.Err(err))
})

// raw message callback (called before event dispatch, for every message)
h.OnMessage(func(conn *websocket.Conn, data []byte) {
    logger.Infof("raw message received: %s", string(data))
})

// register event handlers
// triggered when a client sends {"type":"chat","data":{"text":"hello"}}
h.Handle("chat", func(conn *websocket.Conn, data json.RawMessage) {
    var msg struct{ Text string `json:"text"` }
    _ = json.Unmarshal(data, &msg)
    // handle the message...
})

// multiple events
h.Handle("join", func(conn *websocket.Conn, data json.RawMessage) {
    // ...
}).Handle("leave", func(conn *websocket.Conn, data json.RawMessage) {
    // ...
})
```

### Event

`Event` is the `{type, data}` event structure used for event-driven messaging:

```go
// create an event
e, err := websocket.NewEvent("chat", map[string]string{"msg": "hello"})
// => {"type":"chat","data":{"msg":"hello"}}

// send through a connection
conn.Emit("chat", map[string]string{"msg": "hello"})

// send through a broadcaster
srv.To("room1").Emit("chat", map[string]string{"msg": "hello"})

// decode the data inside an event handler
h.Handle("chat", func(conn *websocket.Conn, data json.RawMessage) {
    var msg struct{ Text string `json:"text"` }
    _ = json.Unmarshal(data, &msg) // data is a json.RawMessage with no Unmarshal method; use json.Unmarshal
})
```

### Room

The `Room` interface provides room management with both in-memory and Redis implementations:

```go
// in-memory rooms (single node)
room := websocket.NewMemoryRoom()

// Redis rooms (distributed)
room := websocket.NewRedisRoom(rdb, "ws:room:")

// operations
room.Add(1, "room1", "room2")           // add connection 1 to the rooms
room.Delete(1, "room1")                  // remove connection 1 from room1
room.Delete(1)                           // remove connection 1 from every room
clients := room.GetClients("room1")     // list the connection IDs in a room
rooms := room.GetRooms(1)               // all rooms connection 1 is in
room.Clear()                             // clear every room (⚠️ ops/testing only, see below)
```

> ⚠️ **`Clear` affects every instance**: the Redis room keys (`{prefix}rooms:*` / `{prefix}fds:*`)
> are shared by every instance in the cluster, so `Clear` also wipes the connections of **other
> nodes** from the rooms, silently breaking their later broadcasts. It should therefore only be
> used for operations/testing.
>
> `Server.Close()` does **not** call `Clear`; it only removes this instance's connections from the
> rooms (cleaning up one by one through `room.Delete(fd)`) without affecting other nodes.

### Write timeout and slow clients

A write deadline (`WriteTimeout`, 10 seconds by default) is set before every write. This is
necessary: if a client stops reading, its TCP buffer fills up and `WriteMessage` **blocks
forever**, and while blocked it holds that connection's write lock, which in turn stalls:

- the broadcast loop (blocking the caller's business goroutine)
- `Conn.Close()` and, through it, the whole `Server.Close()`
- that connection's heartbeat goroutine

Once `WriteTimeout` elapses the write returns an error (usually `i/o timeout`), the lock is
released, and the flows above can continue. The default is generous enough for normal clients and
needs no tuning; only scenarios such as a very poor network or extremely large single messages
require a larger value.

## Configuration

### Configuration fields

| Field | Type | Default | Description |
| ------ | ------ | -------- | ------ |
| `PingInterval` | `time.Duration` | `25s` | Heartbeat interval |
| `PingTimeout` | `time.Duration` | `60s` | Heartbeat timeout |
| `ReadBufferSize` | `int` | `4096` | Read buffer size (bytes) |
| `WriteBufferSize` | `int` | `4096` | Write buffer size (bytes) |
| `WriteTimeout` | `time.Duration` | `10s` | Timeout for a single write; set to a negative value to disable |
| `MaxMessageSize` | `int64` | `4096` | Maximum size of a single message (bytes) |
| `NodeID` | `uint16` | `0` | Node ID; every instance must differ in a cluster |
| `RoomType` | `string` | `memory` | Room storage type (`memory` or `redis`) |
| `RoomPrefix` | `string` | `ws:room:` | Redis room key prefix |
| `RedisAddr` | `string` | `127.0.0.1:6379` | Redis address |
| `RedisPassword` | `string` | `""` | Redis password |
| `RedisDB` | `int` | `0` | Redis database number |

### Message type constants

| Constant | Value | Description |
| ------ | ---- | ------ |
| `TextMessage` | `1` | Text message |
| `BinaryMessage` | `2` | Binary message |
| `CloseMessage` | `3` | Close message |
| `PingMessage` | `9` | Ping message |
| `PongMessage` | `10` | Pong message |

## Architecture

```text
┌──────────────────────────────────────────────────────┐
│                     Server                            │
│  ┌──────────────┐  ┌──────────────┐  ┌────────────┐  │
│  │  Conns Map   │  │    Room      │  │  Handler   │  │
│  │  (ID → Conn) │  │ (interface)  │  │ (interface)│  │
│  └──────────────┘  └──────────────┘  └────────────┘  │
│         │                  │                │         │
│         ▼                  ▼                ▼         │
│  ┌──────────────┐  ┌──────────────┐  ┌────────────┐  │
│  │   Conn       │  │ MemoryRoom / │  │EventHandler│  │
│  │ (gorilla/ws) │  │ RedisRoom    │  │  (default)  │  │
│  └──────────────┘  └──────────────┘  └────────────┘  │
│                                                       │
│  ┌──────────────────────────────────────────────────┐ │
│  │              Broadcaster (fluent API)             │ │
│  │  srv.To("room1", "room2").PushText("hello")       │ │
│  │  srv.To("room1").Emit("event", data)             │ │
│  └──────────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────────┘
```

### Heartbeat mechanism

Every `PingInterval` (25 seconds by default) the server sends a Ping frame to the client:

1. The client replies with a Pong automatically (handled by the browser/SDK)
2. On receiving the Pong the server resets the read deadline to `PingTimeout` (60 seconds by default)
3. If neither a message nor a Pong arrives within `PingTimeout`, the connection is closed

### Room implementations

**MemoryRoom** (in-memory rooms):

- Two maps maintain the bidirectional mapping between `room → fds` and `fd → rooms`
- `sync.RWMutex` keeps it safe for concurrent use
- Suited to single-node deployments

**RedisRoom** (Redis rooms):

- Redis SETs maintain the mappings: `{prefix}rooms:{room}` and `{prefix}fds:{fd}`
- The sets are managed with `SADD/SREM/SMEMBERS`
- Suited to multi-instance deployments, where processes share Redis for cross-instance broadcast

### Cluster deployment

Cluster deployment has to solve two problems:

1. **Globally unique connection IDs**: every instance sets a different `NodeID`, and connection IDs
   are encoded as `nodeID<<32 | localCounter`, so IDs from different instances never overlap.

   ```go
   // instance A: NodeID=1 → conn IDs = 4294967297, 4294967298, ...
   // instance B: NodeID=2 → conn IDs = 8589934593, 8589934594, ...
   ```

2. **Cross-instance broadcast**: Redis Pub/Sub delivers messages across instances.
   When instance A calls `srv.To("lobby").PushText("hello")`:
   - the message is published to the cluster channel with Redis `PUBLISH`
   - the ClusterHandler of every instance (including A itself) receives it
   - each instance dispatches to its local connections according to its local room members

   ```go
   // the Redis room mode enables cluster broadcast automatically
   srv := websocket.MustNew(websocket.Config{
       RoomType: "redis",
       NodeID:   1, // different per instance
   }, handler, websocket.WithRedisClient(rdb))

   // a custom implementation can also be injected with WithPubSub
   srv := websocket.MustNew(websocket.Config{
       NodeID: 1,
   }, handler, websocket.WithPubSub(myPubSub))
   ```

## Error handling

| Error | Description |
| ------ | ------ |
| `ErrConnClosed` | The connection is already closed |

```go
err := conn.WriteText(data)
if errors.Is(err, websocket.ErrConnClosed) {
    // the connection is already closed
}
```

## Full example

### Chat room

```go
package main

import (
    "encoding/json"
    "net/http"

    "github.com/chihqiang/infra-go/logger"
    "github.com/chihqiang/infra-go/websocket"
)

type ChatMessage struct {
    User string `json:"user"`
    Text string `json:"text"`
}

func main() {
    handler := websocket.NewEventHandler()

    // a user joins the chat room
    handler.OnOpen(func(conn *websocket.Conn) {
        conn.Join("chatroom")
        conn.Emit("system", map[string]string{
            "msg": "Welcome to the chat room",
        })
    })

    // a chat message arrived: broadcast it to everyone in the chat room
    handler.Handle("message", func(conn *websocket.Conn, data json.RawMessage) {
        var msg ChatMessage
        _ = json.Unmarshal(data, &msg)
        // broadcast to the chatroom room
        _ = conn.Server().To("chatroom").Emit("message", msg)
    })

    // a user leaves
    handler.OnClose(func(conn *websocket.Conn, err error) {
        // notify the chat room
        _ = conn.Server().To("chatroom").Emit("system", map[string]string{
            "msg": "A user has left the chat room",
        })
    })

    srv := websocket.MustNew(websocket.Config{}, handler)
    defer srv.Close()

    http.Handle("/ws", srv)
    logger.Fatal("server failed", logger.Err(http.ListenAndServe(":8080", nil)))
}
```

### Corresponding frontend JavaScript

```javascript
const ws = new WebSocket("ws://localhost:8080/ws");

ws.onopen = () => {
    // system message received: {"type":"system","data":{"msg":"Welcome to the chat room"}}
};

ws.onmessage = (event) => {
    const data = JSON.parse(event.data);
    console.log(data.type, data.data);
};

// send a message
ws.send(JSON.stringify({
    type: "message",
    data: { user: "Alice", text: "Hello!" }
}));
```
