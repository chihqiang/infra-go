# websocket

基于 [gorilla/websocket](https://github.com/gorilla/websocket) 的 WebSocket 服务封装，提供连接管理、事件分发、房间广播和心跳检测等功能。

## 特性

- **事件驱动**：`EventHandler` 自动将 JSON `{type, data}` 消息分发到注册的处理器
- **房间系统**：内存 + Redis 双后端，支持 `Join/Leave` 和按房间广播
- **集群支持**：通过 Redis Pub/Sub 实现跨实例广播，连接 ID 全局唯一
- **流畅广播**：`srv.To("room1", "room2").PushText("hello")` 一行完成定向推送
- **心跳检测**：基于 Ping/Pong 的自动心跳，超时自动断开
- **线程安全**：所有写操作通过 mutex 保护，支持并发写入
- **用户值存储**：`conn.Set("userID", "xxx")` 方便在 Handler 间传递上下文
- **配置驱动**：Config 通过 `default` 结构体标签定义默认值，遵循 conf 标准
- **统一风格**：中文注释、英文错误信息、函数式选项配置，与 infra-go 其他模块一致

## 安装

```bash
go get github.com/chihqiang/infra-go/websocket
```

## 快速开始

### 基础示例

```go
package main

import (
    "encoding/json"
    "log"
    "net/http"

    "github.com/chihqiang/infra-go/websocket"
)

func main() {
    // 创建事件驱动处理器
    handler := websocket.NewEventHandler()

    // 连接建立
    handler.OnOpen(func(conn *websocket.Conn) {
        log.Printf("连接建立: %d", conn.ID())
        conn.Set("joinedAt", time.Now())
    })

    // 事件处理：聊天消息
    handler.Handle("chat", func(conn *websocket.Conn, data json.RawMessage) {
        var msg struct {
            Text string `json:"text"`
        }
        _ = json.Unmarshal(data, &msg)

        // 广播给所有连接
        conn.Server().BroadcastEvent("message", map[string]string{
            "text":     msg.Text,
            "sender":   "anonymous",
        })
    })

    // 事件处理：加入房间
    handler.Handle("join", func(conn *websocket.Conn, data json.RawMessage) {
        var room string
        _ = json.Unmarshal(data, &room)
        conn.Join(room)
        conn.Emit("joined", room)
    })

    // 连接关闭
    handler.OnClose(func(conn *websocket.Conn, err error) {
        log.Printf("连接关闭: %d", conn.ID())
    })

    // 创建服务器
    srv := websocket.MustNew(websocket.Config{}, handler)
    defer srv.Close()

    http.Handle("/ws", srv)
    log.Fatal(http.ListenAndServe(":8080", nil))
}
```

### Redis 房间（多实例部署）

集群部署时，每个实例设置不同的 `NodeID`，连接 ID 编码为 `nodeID<<32 | localCounter`，
保证全局唯一。通过 Redis Pub/Sub 自动实现跨实例广播：

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
        // 广播到指定房间（自动跨实例）
        conn.Server().To("lobby").Emit("message", msg)
    })

    // 使用 Redis 房间 + NodeID，多实例共享房间状态和广播
    srv := websocket.MustNew(websocket.Config{
        RoomType: "redis",
        NodeID:   1, // 每个实例设置不同的 NodeID
    }, handler, websocket.WithRedisClient(rdb))
    defer srv.Close()

    http.Handle("/ws", srv)
    http.ListenAndServe(":8080", nil)
}
```

### 自定义 Handler

实现 `Handler` 接口可以完全控制连接生命周期：

```go
type myHandler struct{}

func (h *myHandler) HandleOpen(conn *websocket.Conn) {
    log.Printf("连接建立: %d", conn.ID())
}

func (h *myHandler) HandleMessage(conn *websocket.Conn, messageType int, data []byte) {
    // 原始消息处理（非事件驱动）
    _ = conn.WriteMessage(messageType, data) // echo
}

func (h *myHandler) HandleClose(conn *websocket.Conn, err error) {
    log.Printf("连接关闭: %d", conn.ID())
}

func (h *myHandler) HandleError(conn *websocket.Conn, err error) {
    log.Printf("连接错误: %d, %v", conn.ID(), err)
}

func main() {
    srv := websocket.MustNew(websocket.Config{}, &myHandler{})
    http.Handle("/ws", srv)
    http.ListenAndServe(":8080", nil)
}
```

## API

### 创建服务器

```go
// 创建服务器（返回 error）
srv, err := websocket.New(websocket.Config{}, handler)

// 创建服务器（出错 panic，适合全局初始化）
srv := websocket.MustNew(websocket.Config{}, handler)

// 使用选项
srv := websocket.MustNew(websocket.Config{}, handler,
    websocket.WithLogger(myLogger),
    websocket.WithCheckOrigin(func(r *http.Request) bool {
        return r.Host == "example.com"
    }),
)
```

### 选项

| 选项 | 说明 |
| ------ | ------ |
| `WithLogger(l logger.ILogger)` | 设置日志记录器，默认使用 `logger.GetGlobal()` |
| `WithRedisClient(client)` | 设置 Redis 客户端（用于 Redis 房间） |
| `WithPubSub(ps)` | 设置自定义 PubSub 实现（用于集群广播） |
| `WithCheckOrigin(fn)` | 设置 Origin 检查函数，默认允许所有来源 |
| `WithSubprotocols(protocols...)` | 设置子协议协商列表 |

### Server 方法

```go
// 连接管理
conn, ok := srv.GetConn(id)     // 根据连接 ID 获取连接
count := srv.Count()            // 当前在线连接数
err := srv.CloseConn(id)        // 关闭指定连接
err := srv.Close()              // 关闭服务器（断开所有连接）

// 广播
srv.Broadcast(data)            // 广播文本消息到所有连接
srv.BroadcastText("hello")      // 广播字符串到所有连接
srv.BroadcastJSON(v)           // 广播 JSON 到所有连接
srv.BroadcastEvent("news", v)   // 广播事件到所有连接

// 定向广播（返回 Broadcaster）
srv.To("room1", "room2").PushText("hello")
srv.To("room1").Emit("event", data)
srv.To("room1").WriteJSON(v)

// 房间管理
room := srv.Room()              // 获取房间管理器
clients := room.GetClients("room1") // 获取房间内的连接 ID 列表
```

### Conn 方法

```go
// 连接信息
id := conn.ID()                 // 连接唯一 ID
srv := conn.Server()            // 所属服务器
addr := conn.RemoteAddr()       // 客户端地址
req := conn.Request()           // 原始 HTTP 请求（只读）

// 发送消息
conn.WriteText(data)            // 发送文本消息
conn.WriteTextString("hello")   // 发送字符串文本消息
conn.WriteBinary(data)          // 发送二进制消息
conn.WriteJSON(v)               // 发送 JSON 消息
conn.WriteMessage(type, data)    // 发送指定类型消息
conn.Emit("event", data)        // 发送事件 {"type":"event","data":...}

// 房间操作
conn.Join("room1", "room2")    // 加入房间
conn.Leave("room1")              // 离开房间
rooms := conn.Rooms()           // 当前所在的所有房间

// 用户值
conn.Set("userID", "user-123")  // 设置用户值
val, ok := conn.Get("userID")   // 获取用户值
val := conn.MustGet("userID")   // 获取用户值（不存在返回 nil）

// 关闭
conn.Close()                    // 关闭连接
conn.IsClosed()                 // 连接是否已关闭
```

### EventHandler

`EventHandler` 是默认的 `Handler` 实现，支持事件驱动的消息处理：

```go
h := websocket.NewEventHandler()

// 链式注册回调
h.OnOpen(func(conn *websocket.Conn) {
    log.Printf("连接建立: %d", conn.ID())
}).OnClose(func(conn *websocket.Conn, err error) {
    log.Printf("连接关闭: %d", conn.ID())
}).OnError(func(conn *websocket.Conn, err error) {
    log.Printf("错误: %v", err)
})

// 原始消息回调（在事件分发之前调用，对每条消息生效）
h.OnMessage(func(conn *websocket.Conn, data []byte) {
    log.Printf("收到原始消息: %s", string(data))
})

// 注册事件处理器
// 客户端发送 {"type":"chat","data":{"text":"hello"}} 时触发
h.Handle("chat", func(conn *websocket.Conn, data json.RawMessage) {
    var msg struct{ Text string `json:"text"` }
    _ = json.Unmarshal(data, &msg)
    // 处理消息...
})

// 多个事件
h.Handle("join", func(conn *websocket.Conn, data json.RawMessage) {
    // ...
}).Handle("leave", func(conn *websocket.Conn, data json.RawMessage) {
    // ...
})
```

### Event

`Event` 是 `{type, data}` 格式的事件结构，用于事件驱动的消息通信：

```go
// 创建事件
e, err := websocket.NewEvent("chat", map[string]string{"msg": "hello"})
// => {"type":"chat","data":{"msg":"hello"}}

// 通过连接发送
conn.Emit("chat", map[string]string{"msg": "hello"})

// 通过广播器发送
srv.To("room1").Emit("chat", map[string]string{"msg": "hello"})

// 在事件处理器中解码数据
h.Handle("chat", func(conn *websocket.Conn, data json.RawMessage) {
    var msg struct{ Text string `json:"text"` }
    _ = data.Unmarshal(data, &msg) // 或用 event.Decode(&msg)
})
```

### Room

`Room` 接口提供房间管理，支持内存和 Redis 两种实现：

```go
// 内存房间（单机）
room := websocket.NewMemoryRoom()

// Redis 房间（分布式）
room := websocket.NewRedisRoom(rdb, "ws:room:")

// 操作
room.Add(1, "room1", "room2")           // 将连接 1 加入房间
room.Delete(1, "room1")                  // 将连接 1 从 room1 移除
room.Delete(1)                           // 移除连接 1 的所有房间
clients := room.GetClients("room1")     // 获取房间内的连接 ID 列表
rooms := room.GetRooms(1)               // 获取连接 1 所在的所有房间
room.Clear()                             // 清空所有房间
```

## 配置

### 配置项说明

| 字段 | 类型 | 默认值 | 说明 |
| ------ | ------ | -------- | ------ |
| `PingInterval` | `time.Duration` | `25s` | 心跳检测间隔 |
| `PingTimeout` | `time.Duration` | `60s` | 心跳超时时间 |
| `ReadBufferSize` | `int` | `4096` | 读缓冲区大小（字节） |
| `WriteBufferSize` | `int` | `4096` | 写缓冲区大小（字节） |
| `MaxMessageSize` | `int64` | `4096` | 单条消息最大大小（字节） |
| `NodeID` | `uint16` | `0` | 节点 ID，集群部署时每个实例必须不同 |
| `RoomType` | `string` | `memory` | 房间存储类型（`memory` 或 `redis`） |
| `RoomPrefix` | `string` | `ws:room:` | Redis 房间键前缀 |
| `RedisAddr` | `string` | `127.0.0.1:6379` | Redis 地址 |
| `RedisPassword` | `string` | `""` | Redis 密码 |
| `RedisDB` | `int` | `0` | Redis 数据库编号 |

### 消息类型常量

| 常量 | 值 | 说明 |
| ------ | ---- | ------ |
| `TextMessage` | `1` | 文本消息 |
| `BinaryMessage` | `2` | 二进制消息 |
| `CloseMessage` | `3` | 关闭消息 |
| `PingMessage` | `9` | Ping 消息 |
| `PongMessage` | `10` | Pong 消息 |

## 架构设计

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

### 心跳机制

服务器每隔 `PingInterval`（默认 25 秒）向客户端发送 Ping 帧：

1. 客户端收到 Ping 后自动回复 Pong（由浏览器/SDK 自动处理）
2. 服务器收到 Pong 后重置读超时为 `PingTimeout`（默认 60 秒）
3. 如果 `PingTimeout` 内未收到任何消息或 Pong，则断开连接

### Room 实现

**MemoryRoom**（内存房间）：

- 使用两个 map 维护 `room → fds` 和 `fd → rooms` 的双向映射
- 通过 `sync.RWMutex` 保证并发安全
- 适用于单机部署

**RedisRoom**（Redis 房间）：

- 使用 Redis SET 维护映射：`{prefix}rooms:{room}` 和 `{prefix}fds:{fd}`
- 通过 `SADD/SREM/SMEMBERS` 管理集合
- 适用于多实例部署，不同进程通过共享 Redis 实现跨实例广播

### 集群部署

集群部署时需要解决两个问题：

1. **连接 ID 全局唯一**：每个实例设置不同的 `NodeID`，
   连接 ID 编码为 `nodeID<<32 | localCounter`，保证不同实例的 ID 不重叠。

   ```go
   // 实例 A: NodeID=1 → 连接 ID = 4294967297, 4294967298, ...
   // 实例 B: NodeID=2 → 连接 ID = 8589934593, 8589934594, ...
   ```

2. **跨实例广播**：通过 Redis Pub/Sub 实现消息跨实例投递。
   当实例 A 调用 `srv.To("lobby").PushText("hello")` 时：
   - 消息通过 Redis `PUBLISH` 发送到集群频道
   - 所有实例（包括 A 自身）的 ClusterHandler 收到消息
   - 各实例根据本地房间成员分发到本地连接

   ```go
   // Redis 房间模式自动启用集群广播
   srv := websocket.MustNew(websocket.Config{
       RoomType: "redis",
       NodeID:   1, // 每个实例不同
   }, handler, websocket.WithRedisClient(rdb))

   // 也可通过 WithPubSub 注入自定义实现
   srv := websocket.MustNew(websocket.Config{
       NodeID: 1,
   }, handler, websocket.WithPubSub(myPubSub))
   ```

## 错误处理

| 错误 | 说明 |
| ------ | ------ |
| `ErrConnClosed` | 连接已关闭 |

```go
err := conn.WriteText(data)
if errors.Is(err, websocket.ErrConnClosed) {
    // 连接已关闭
}
```

## 完整示例

### 聊天室

```go
package main

import (
    "encoding/json"
    "log"
    "net/http"

    "github.com/chihqiang/infra-go/websocket"
)

type ChatMessage struct {
    User string `json:"user"`
    Text string `json:"text"`
}

func main() {
    handler := websocket.NewEventHandler()

    // 用户加入聊天室
    handler.OnOpen(func(conn *websocket.Conn) {
        conn.Join("chatroom")
        conn.Emit("system", map[string]string{
            "msg": "欢迎加入聊天室",
        })
    })

    // 收到聊天消息，广播给聊天室所有成员
    handler.Handle("message", func(conn *websocket.Conn, data json.RawMessage) {
        var msg ChatMessage
        _ = json.Unmarshal(data, &msg)
        // 广播到 chatroom 房间
        _ = conn.Server().To("chatroom").Emit("message", msg)
    })

    // 用户离开
    handler.OnClose(func(conn *websocket.Conn, err error) {
        // 通知聊天室
        _ = conn.Server().To("chatroom").Emit("system", map[string]string{
            "msg": "有用户离开了聊天室",
        })
    })

    srv := websocket.MustNew(websocket.Config{}, handler)
    defer srv.Close()

    http.Handle("/ws", srv)
    log.Fatal(http.ListenAndServe(":8080", nil))
}
```

### 前端 JavaScript 对应

```javascript
const ws = new WebSocket("ws://localhost:8080/ws");

ws.onopen = () => {
    // 收到系统消息: {"type":"system","data":{"msg":"欢迎加入聊天室"}}
};

ws.onmessage = (event) => {
    const data = JSON.parse(event.data);
    console.log(data.type, data.data);
};

// 发送消息
ws.send(JSON.stringify({
    type: "message",
    data: { user: "Alice", text: "Hello!" }
}));
```
