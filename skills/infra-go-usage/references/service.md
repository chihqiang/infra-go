# service

A service group manager that starts and stops several services concurrently, with panic
recovery and graceful shutdown.

## Features

- **Concurrent start**: all Services call `Start` concurrently; blocks until all of them exit
- **Concurrent stop**: all Services call `Stop` concurrently, with `sync.Once` guaranteeing it
  runs only once
- **Panic recovery**: panics in both `Start` and `Stop` are recovered and logged through
  `logger` without interrupting other services; a panic in `Start` automatically triggers `Stop`
  to unblock the other services
- **Adapters**: `WithStart` / `WithStarter` wrap plain functions; `AsService` adapts objects
  with `Start() error` + `Stop() error` (such as `httpx.Server`)
- **Logger integration**: errors are recorded with the global `infra-go/logger`

## Installation

```bash
go get github.com/chihqiang/infra-go/service
```

## Core interfaces

```go
// Starter starts a service
type Starter interface {
    Start()
}

// Stopper stops a service
type Stopper interface {
    Stop()
}

// Service can both Start and Stop
type Service interface {
    Starter
    Stopper
}
```

## Quick start

```go
package main

import (
    "net/http"

    "github.com/chihqiang/infra-go/httpx"
    "github.com/chihqiang/infra-go/service"
)

func main() {
    sg := service.NewServiceGroup()

    // HTTP service
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

    // Adapt *httpx.Server into a Service with service.AsService for direct management
    // (AsService adapts objects with Start() error + Stop() error; Start blocks until the signal
    // arrives and triggers a graceful shutdown)
    sg.Add(service.AsService(server))

    // start all services (blocking)
    sg.Start()
}
```

## Managing multiple services

```go
sg := service.NewServiceGroup()

// Adding several services:
// - objects shaped like *httpx.Server (Start() error + Stop() error) go straight through
//   service.AsService
// - plain functions with no stop capability, or that handle the exit signal themselves, use
//   service.WithStart
sg.Add(service.AsService(server))                  // HTTP service (httpx.Server)
sg.Add(service.WithStart(func() { _ = consumer.Run() })) // Redis consumer (asynq)
sg.Add(service.WithStart(func() { cronTick() }))  // cron job

// start concurrently, blocking until all of them exit
sg.Start()

// stop manually (usually called from a signal handler)
sg.Stop()
```

## Custom Service

Implement the `Service` interface:

```go
type MyService struct {
    stopCh chan struct{}
}

func (s *MyService) Start() {
    <-s.stopCh // blocks until Stop
}

func (s *MyService) Stop() {
    close(s.stopCh) // unblocks Start
}

// usage
sg.Add(&MyService{stopCh: make(chan struct{})})
```

## Adapter functions

### WithStart

Wraps a plain `func()` into a Service (`Stop` is a no-op):

```go
sg.Add(service.WithStart(func() {
    // start-up logic (Start returns immediately when it is non-blocking)
    runWorker()
}))
```

### WithStarter

Wraps a `Starter` interface into a Service (`Stop` is a no-op):

```go
type MyStarter struct{}
func (s *MyStarter) Start() { ... }

sg.Add(service.WithStarter(&MyStarter{}))
```

### AsService

Adapts objects that implement `Start() error` and `Stop() error` into a Service, ready for
`sg.Add`. A typical object is `httpx.Server` (both its `Start` and `Stop` return an error, so the
signatures differ from `Service.Starter`/`Stopper` and it cannot be used as a Service directly):

```go
server := httpx.NewServer(httpx.ServerConfig{Host: "0.0.0.0", Port: 8080})
// ...register routes...

sg.Add(service.AsService(server)) // managed together by the ServiceGroup
```

The errors returned by `Start` / `Stop` are both logged (the `Service` interface has no return
value, so they cannot be propagated); a panic in `Start` is still recovered by the ServiceGroup,
which triggers `Stop`.

## Panic handling

Panics never crash the program; they are all recorded through the global `logger`.

### Panic in Start

If a Service panics inside `Start`:

1. The panic is recovered
2. The error is logged via `logger.Errorf`
3. `Stop()` is triggered automatically to stop all other services (unblocking them)
4. `Start()` returns normally and does not re-panic

```go
sg := service.NewServiceGroup()
sg.Add(normalService)     // a normal service
sg.Add(panicService)      // panics inside Start

sg.Start() // returns normally, no panic
// log output format (the actual format used by the source):
// {"level":"ERROR",...,"msg":"service: panic during start, index: <idx>, type: <service type>, reason: <panic value>"}

// normalService has been stopped
```

### Panic in Stop

If a Service panics inside `Stop`:

1. The panic is recovered
2. The error is logged via `logger.Errorf`
3. Stopping of the other services is unaffected

```go
sg.Stop() // returns normally; the panic was logged
// log output format (the actual format used by the source):
// {"level":"ERROR",...,"msg":"service: panic during stop, index: <idx>, type: <service type>, reason: <panic value>"}
```

## API

### ServiceGroup

| Method | Description |
| ------ | ------ |
| `NewServiceGroup()` | Create a service group |
| `Add(service)` | Add a service (appended to the end; start follows the addition order, stop runs in reverse) |
| `Start()` | Start all services concurrently; blocks until all of them exit |
| `Stop()` | Stop all services concurrently, guaranteed to run only once |

### Adapter function API

| Function | Description |
| ------ | ------ |
| `WithStart(fn)` | Wrap a `func()` into a Service (Stop is a no-op) |
| `WithStarter(s)` | Wrap a `Starter` into a Service (Stop is a no-op) |
| `AsService(s)` | Adapt an object with `Start() error` + `Stop() error` (such as `httpx.Server`) into a Service |

## Stop order

`Add` appends services to the end and startup follows the addition order; `Stop` walks the list
in reverse (the ones added last stop first):

```text
Add(A)  → services: [A]
Add(B)  → services: [A, B]
Add(C)  → services: [A, B, C]

Stop order: C → B → A (reverse, but executed concurrently, so the exact order is not guaranteed)
```
