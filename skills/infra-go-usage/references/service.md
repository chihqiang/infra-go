# service

服务组管理包，支持并发启动、并发停止多个服务，Panic 恢复和优雅关闭。

## 特性

- **并发启动**：所有 Service 并发调用 `Start`，阻塞直到全部退出
- **并发停止**：所有 Service 并发调用 `Stop`，`sync.Once` 保证只执行一次
- **Panic 恢复**：`Start` 和 `Stop` 中的 panic 都会被恢复，通过 `logger` 记录错误日志，不中断其他服务；`Start` 中 panic 自动触发 `Stop` 解除其他服务阻塞
- **适配函数**：`WithStart` / `WithStarter` 包装普通函数；`AsService` 适配 `Start() error` + `Stop() error` 对象（如 `httpx.Server`）
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

    // 用 service.AsService 将 *httpx.Server 适配为 Service 直接管理
    // （AsService 适配 Start() error + Stop() error 的对象；Start 阻塞直到收到信号优雅关闭）
    sg.Add(service.AsService(server))

    // 启动所有服务（阻塞）
    sg.Start()
}
```

## 多服务管理

```go
sg := service.NewServiceGroup()

// 添加多个服务：
// - 形如 *httpx.Server（Start() error + Stop() error）的对象用 service.AsService 直接适配
// - 无停止能力/需自行处理退出信号的普通函数用 service.WithStart
sg.Add(service.AsService(server))                  // HTTP 服务（httpx.Server）
sg.Add(service.WithStart(func() { _ = consumer.Run() })) // Redis 消费者（asynq）
sg.Add(service.WithStart(func() { cronTick() }))  // 定时任务

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

### AsService

将实现了 `Start() error` 与 `Stop() error` 的对象适配为 Service，可直接 `sg.Add`。
典型对象如 `httpx.Server`（其 `Start` / `Stop` 均返回 error，签名与 `Service.Starter`/`Stopper` 不同，无法直接作为 Service）：

```go
server := httpx.NewServer(httpx.ServerConfig{Host: "0.0.0.0", Port: 8080})
// ...注册路由...

sg.Add(service.AsService(server)) // 纳入 ServiceGroup 统一管理
```

`Start` / `Stop` 返回的 error 均记录为日志（`Service` 接口无返回值，无法向上传递）；`Start` 中 panic 仍由 ServiceGroup 恢复并触发 `Stop`。

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
// 日志输出格式（源码实际格式）:
// {"level":"ERROR",...,"msg":"service: panic during start, index: <idx>, type: <服务类型>, reason: <panic 值>"}

// normalService 已被 Stop
```

### Stop 中 Panic

如果某个 Service 在 `Stop` 中 panic：

1. Panic 被恢复
2. 通过 `logger.Errorf` 记录错误日志
3. 不影响其他服务的停止

```go
sg.Stop() // 正常返回，panic 被记录
// 日志输出格式（源码实际格式）:
// {"level":"ERROR",...,"msg":"service: panic during stop, index: <idx>, type: <服务类型>, reason: <panic 值>"}
```

## API

### ServiceGroup

| 方法 | 说明 |
| ------ | ------ |
| `NewServiceGroup()` | 创建服务组 |
| `Add(service)` | 添加服务（追加到尾部；启动按添加顺序，停止按逆序） |
| `Start()` | 并发启动所有服务，阻塞直到全部退出 |
| `Stop()` | 并发停止所有服务，保证只执行一次 |

### 适配函数 API

| 函数 | 说明 |
| ------ | ------ |
| `WithStart(fn)` | 将 `func()` 包装为 Service（Stop 空操作） |
| `WithStarter(s)` | 将 `Starter` 包装为 Service（Stop 空操作） |
| `AsService(s)` | 将 `Start() error` + `Stop() error` 对象（如 `httpx.Server`）适配为 Service |

## 停止顺序

`Add` 将服务追加到尾部，启动按添加顺序执行；`Stop` 逆序遍历（后添加的先停止）：

```text
Add(A)  → services: [A]
Add(B)  → services: [A, B]
Add(C)  → services: [A, B, C]

Stop 顺序: C → B → A（逆序，但并发执行，不保证精确顺序）
```

## 目录结构

```text
service/
├── servicegroup.go      — ServiceGroup、Service 接口、WithStart/WithStarter/AsService
└── servicegroup_test.go — ServiceGroup 单元测试
```
