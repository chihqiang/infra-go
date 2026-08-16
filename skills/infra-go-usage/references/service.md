# service

服务组管理包，支持并发启动、并发停止多个服务，Panic 恢复和优雅关闭。

## 特性

- **并发启动**：所有 Service 并发调用 `Start`，阻塞直到全部退出
- **并发停止**：所有 Service 并发调用 `Stop`，`sync.Once` 保证只执行一次
- **Panic 恢复**：`Start` 和 `Stop` 中的 panic 都会被恢复，通过 `logger` 记录错误日志，不中断其他服务；`Start` 中 panic 自动触发 `Stop` 解除其他服务阻塞
- **适配函数**：`WithStart` / `WithStarter` 将普通函数包装为 Service
- **日志集成**：使用 `infra-go/logger` 全局日志记录错误

## 安装

```bash
go get github.com/chihqiang/infra-go/service
```

## 核心接口

```go
// Starter 启动服务
type Starter interface {
    Start()
}

// Stopper 停止服务
type Stopper interface {
    Stop()
}

// Service 同时具备 Start 和 Stop 能力
type Service interface {
    Starter
    Stopper
}
```

## 快速开始

```go
package main

import (
    "fmt"
    "net/http"

    "github.com/chihqiang/infra-go/httpx"
    "github.com/chihqiang/infra-go/service"
)

func main() {
    sg := service.NewServiceGroup()

    // HTTP 服务
    server := httpx.NewServer(httpx.ServerConfig{
        Host: "0.0.0.0",
        Port: 8080,
    })
    server.AddRoute(httpx.Route{
        Method: "GET", Path: "/ping",
        Handler: func(w http.ResponseWriter, r *http.Request) {
            httpx.OkJSON(w, "pong")
        },
    })

    // 用 WithStart 包装为 Service（Start 阻塞，Stop 调用 server.Shutdown）
    sg.Add(service.WithStart(func() {
        fmt.Println("HTTP server starting on :8080")
        server.Start()
    }))

    // 启动所有服务（阻塞）
    sg.Start()
}
```

## 多服务管理

```go
sg := service.NewServiceGroup()

// 添加多个服务
sg.Add(httpService)     // HTTP 服务
sg.Add(redisService)   // Redis 消费者
sg.Add(cronService)    // 定时任务

// 并发启动，阻塞直到全部退出
sg.Start()

// 手动停止（通常在信号处理中调用）
sg.Stop()
```

## 自定义 Service

实现 `Service` 接口：

```go
type MyService struct {
    stopCh chan struct{}
}

func (s *MyService) Start() {
    <-s.stopCh // 阻塞直到 Stop
}

func (s *MyService) Stop() {
    close(s.stopCh) // 解除 Start 阻塞
}

// 使用
sg.Add(&MyService{stopCh: make(chan struct{})})
```

## 适配函数

### WithStart

将普通 `func()` 包装为 Service（`Stop` 为空操作）：

```go
sg.Add(service.WithStart(func() {
    // 启动逻辑（非阻塞时 Start 立即返回）
    runWorker()
}))
```

### WithStarter

将 `Starter` 接口包装为 Service（`Stop` 为空操作）：

```go
type MyStarter struct{}
func (s *MyStarter) Start() { ... }

sg.Add(service.WithStarter(&MyStarter{}))
```

## Panic 处理

Panic 不会导致程序崩溃，全部通过 `logger` 全局日志记录。

### Start 中 Panic

如果某个 Service 在 `Start` 中 panic：

1. Panic 被恢复
2. 通过 `logger.Errorf` 记录错误日志
3. 自动触发 `Stop()` 停止所有其他服务（解除阻塞）
4. `Start()` 正常返回，不重新 panic

```go
sg := service.NewServiceGroup()
sg.Add(normalService)     // 正常服务
sg.Add(panicService)      // Start 中 panic

sg.Start() // 正常返回，不 panic
// 日志输出: {"level":"ERROR",...,"msg":"service: panic during start: boom"}

// normalService 已被 Stop
```

### Stop 中 Panic

如果某个 Service 在 `Stop` 中 panic：

1. Panic 被恢复
2. 通过 `logger.Errorf` 记录错误日志
3. 不影响其他服务的停止

```go
sg.Stop() // 正常返回，panic 被记录
// 日志输出: {"level":"ERROR",...,"msg":"service: panic during stop: ..."}
```

## API

### ServiceGroup

| 方法 | 说明 |
| ------ | ------ |
| `NewServiceGroup()` | 创建服务组 |
| `Add(service)` | 添加服务（插入头部，逆序停止） |
| `Start()` | 并发启动所有服务，阻塞直到全部退出 |
| `Stop()` | 并发停止所有服务，保证只执行一次 |

### 适配函数 API

| 函数 | 说明 |
| ------ | ------ |
| `WithStart(fn)` | 将 `func()` 包装为 Service |
| `WithStarter(s)` | 将 `Starter` 包装为 Service |

## 停止顺序

`Add` 时服务插入到头部，`Stop` 时按切片顺序停止（即添加的逆序）：

```text
Add(A)  → services: [A]
Add(B)  → services: [B, A]
Add(C)  → services: [C, B, A]

Stop 顺序: C → B → A（但并发执行，不保证精确顺序）
```

## 目录结构

```text
service/
├── servicegroup.go      — ServiceGroup、Service 接口、WithStart/WithStarter
├── servicegroup_test.go — 单元测试
└── README.md
```
