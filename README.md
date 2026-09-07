# infra-go

Go 项目底层基础设施通用封装库，整合存储、日志、配置、工具等基础能力。

> **环境要求**：Go 1.25+（`go.mod` 声明 `go 1.25.11`）

## 快速开始

最小 HTTP 服务示例（配置 `conf` + 日志 `logger` + HTTP `httpx` + 生命周期编排 `service`），零外部服务依赖；需要数据库 / Redis 时再按需引入 `orm` / `redisx`：

```go
package main

import (
    "net/http"
    "github.com/chihqiang/infra-go/conf"
    "github.com/chihqiang/infra-go/httpx"
    "github.com/chihqiang/infra-go/logger"
    "github.com/chihqiang/infra-go/service"
)

// 配置：json 标签声明默认值与约束，支持 conf 从文件 / 环境变量加载
type Config struct {
    Host string `json:",default=0.0.0.0"`
    Port int    `json:",default=8080,range=[1:65535]"`
}

// 请求体：binding 标签做参数校验
type helloRequest struct {
    Name string `json:"name" binding:"required"`
}

func main() {
    // 1. 全局日志（最先初始化）；退出前 Sync 刷缓冲
    logger.SetGlobal(logger.New(logger.Config{
        Level:   logger.InfoLevel,
        AppName: "demo",
    }))
    defer logger.Sync()

    // 2. 配置加载：默认值 / JSON·YAML / 环境变量展开
    var cfg Config
    if err := conf.Load("config.yaml", &cfg, conf.UseEnv()); err != nil {
        logger.Fatal("load config failed", logger.Err(err))
    }

    // 3. HTTP 服务：参数绑定 + 校验 + 统一响应，中间件可插拔
    srv := httpx.NewServer(httpx.ServerConfig{Host: cfg.Host, Port: cfg.Port})
    srv.Use(httpx.WithRequestID(), httpx.WithRecovery()) // 更多见 httpx 模块（限流/追踪/JWT 等）
    srv.AddRoute(httpx.Route{
        Method: "POST",
        Path:   "/hello",
        Handler: func(w http.ResponseWriter, r *http.Request) {
            var req helloRequest
            if err := httpx.MustBindJSON(w, r, &req); err != nil {
                return // 绑定 / 校验失败已自动写 400
            }
            httpx.OkJSON(w, map[string]string{"msg": "hello, " + req.Name})
        },
    })

    // 4. 生命周期编排：并发启停，支持 SIGINT/SIGTERM 优雅关闭
    sg := service.NewServiceGroup()
    sg.Add(service.WithStart(func() { _ = srv.Start() }))
    sg.Start()
}
```

配合 `config.yaml`（可省略，缺省走结构体默认值）：

```yaml
host: 0.0.0.0
port: 8080
```

## 模块

各模块文档统一维护在 [skills/infra-go-usage/references](./skills/infra-go-usage/references)，按模块划分：

| 模块 | 说明 |
| ------ | ------ |
| [conf](./skills/infra-go-usage/references/conf.md) | 配置解析，支持 JSON/YAML，默认值、环境变量、参数验证 |
| [logger](./skills/infra-go-usage/references/logger.md) | 日志封装，基于 zap + lumberjack 滚动日志 |
| [orm](./skills/infra-go-usage/references/orm.md) | ORM 封装，基于 gorm，支持 MySQL/PostgreSQL/SQLite |
| [redisx](./skills/infra-go-usage/references/redisx.md) | Redis 客户端封装，连接池、健康检查、分布式锁 |
| [cache](./skills/infra-go-usage/references/cache.md) | 统一缓存接口，内存（LRU 淘汰、命中率统计）+ Redis（防击穿/防穿透）两种实现 |
| [httpx](./skills/infra-go-usage/references/httpx.md) | HTTP 工具，请求参数绑定、统一泛型响应、路由注册、优雅关闭。内置中间件：CORS/Recovery/RequestID/链路追踪/访问日志/熔断/超时/请求体限制/gzip/并发数限制/限流/JWT 认证/加解密/内容安全。核心按子包拆分：`binding`（绑定实现）、`middleware`（通用中间件，标准 `func(http.Handler) http.Handler`，可被 gin/echo 复用）、`match`（路径匹配）、`respw`（ResponseWriter 包装） |
| [ratelimit](./skills/infra-go-usage/references/ratelimit.md) | 限流器，令牌桶/滑动窗口，内存 + Redis 双后端。HTTP 限流中间件统一为 `httpx.WithRateLimit`（实现见 `httpx/middleware` 子包） |
| [breaker](./skills/infra-go-usage/references/breaker.md) | 熔断器，Google SRE 算法，快速失败、降级、防雪崩 |
| [retry](./skills/infra-go-usage/references/retry.md) | 重试机制，指数退避、固定延迟、抖动 |
| [jwt](./skills/infra-go-usage/references/jwt.md) | JWT 签发与解析，支持 HS256/HS384/HS512（HMAC）；认证中间件 `AuthMiddleware` / `httpx.WithJWT`，验证后注入业务 claims 到 context |
| [hash](./skills/infra-go-usage/references/hash.md) | 哈希加密，MD5/SHA/Bcrypt/HMAC，AES-GCM 加密，HMAC 签名/校验 |
| [trace](./skills/infra-go-usage/references/trace.md) | 链路追踪，基于 OpenTelemetry：agent / span 管理 / gRPC·HTTP 头传播 / 属性封装。HTTP 服务端埋点统一为 `httpx.WithTracing` |
| [mapping](./skills/infra-go-usage/references/mapping.md) | map → struct 反序列化，struct tag 解析引擎 |
| [cast](./skills/infra-go-usage/references/cast.md) | 类型安全转换，支持基本类型/时间/切片/泛型 |
| [syncx](./skills/infra-go-usage/references/syncx.md) | 并发工具，SingleFlight/ConcurrentMap/Semaphore |
| [service](./skills/infra-go-usage/references/service.md) | 服务组，并发启动/停止多个 Service，sync.Once 保证只停一次 |
| [taskq](./skills/infra-go-usage/references/taskq.md) | 异步任务队列，基于 asynq，生产者/消费者模式 |
| [storage](./skills/infra-go-usage/references/storage.md) | 统一对象存储接口，支持阿里云 OSS、腾讯云 COS 和七牛云 KODO。 |
| [websocket](./skills/infra-go-usage/references/websocket.md) | WebSocket 服务封装，基于 gorilla/websocket，事件驱动、房间广播、心跳检测 |
| [stringx](./skills/infra-go-usage/references/stringx.md) | 字符串工具包，随机生成、判断、转换、拆分连接等常用函数 |

## 特性

- **统一风格**：所有模块使用中文注释、英文错误信息、函数式选项配置
- **零依赖侵入**：每个模块独立 import，按需使用
- **类型安全**：广泛使用泛型（`Response[T]`、`cast.To[T]`）
- **可测试**：每个模块都有完整的单元测试，支持 `-race` 检测
- **依赖治理**：统一维护依赖基线并定期升级（当前 Go 1.25 / gorm 1.31 / OpenTelemetry 1.44 等）；模块独立 import、按需引入

## Skills 安装

仓库内置 VS Code Copilot skill —— [infra-go-usage](./skills/infra-go-usage/SKILL.md)，用于指导在业务项目中选型与组装 infra-go 模块（配置、日志、数据库、Redis、HTTP、JWT 等）。

使用 [Agent Skills CLI](https://github.com/vercel-labs/skills)（`npx skills`）一键安装（skill 位于本仓库 `skills/infra-go-usage/`，已推送到远端 `main` 分支）：

```bash
# 安装到当前项目（默认安装到 .claude/skills/ 或 .agents/skills/，自动探测已安装的 agent）
npx skills add chihqiang/infra-go --skill infra-go-usage

# 先预览仓库里能发现哪些 skill（不安装）
npx skills add chihqiang/infra-go --list

# 安装到个人目录（跨项目可用），并指定目标 agent
npx skills add chihqiang/infra-go --skill infra-go-usage -g -a github-copilot
```

### 使用

安装后，在 VS Code 聊天中输入 `/infra-go-usage`（或直接提问，如"用 infra-go 搭一个带登录鉴权和限流的 HTTP 服务"），Copilot 会自动加载该 skill 并按其中的工作流协助你。

> 说明：本仓库将 skill 放在根目录 `skills/` 便于随库分发；VS Code 识别项目级 skill 的标准位置为 `.github/skills/`、`.agents/skills/` 或 `.claude/skills/`。

## 质量验证

```bash
go build ./...                # 编译全部包
go vet ./...                  # 静态检查
go test ./... -race -count=1  # 全量单测 + 竞态检测
```

## License

[Apache-2.0](./LICENSE)
