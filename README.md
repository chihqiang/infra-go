# infra-go

Go 项目底层基础设施通用封装库，整合存储、日志、配置、工具等基础能力。

## 模块

| 模块 | 说明 |
| ------ | ------ |
| [conf](./conf) | 配置解析，支持 JSON/YAML，默认值、环境变量、参数验证 |
| [logger](./logger) | 日志封装，基于 zap + lumberjack 滚动日志 |
| [orm](./orm) | ORM 封装，基于 gorm，支持 MySQL/PostgreSQL/SQLite |
| [redisx](./redisx) | Redis 客户端封装，连接池、健康检查、分布式锁 |
| [httpx](./httpx) | HTTP 工具，请求参数绑定、统一泛型响应、路由注册、中间件链、优雅关闭 |
| [ratelimit](./ratelimit) | 限流器，令牌桶/滑动窗口，内存 + Redis 双后端 |
| [retry](./retry) | 重试机制，指数退避、固定延迟、抖动 |
| [jwt](./jwt) | JWT 签发与解析，支持 RSA/HMAC |
| [hash](./hash) | 哈希加密，MD5/SHA/Bcrypt/HMAC |
| [trace](./trace) | 链路追踪，基于 OpenTelemetry |
| [mapping](./mapping) | map → struct 反序列化，struct tag 解析引擎 |
| [cast](./cast) | 类型安全转换，支持基本类型/时间/切片/泛型 |
| [syncx](./syncx) | 并发工具，SingleFlight/ConcurrentMap/Semaphore |
| [service](./service) | 服务组，并发启动/停止多个 Service，sync.Once 保证只停一次 |
| [taskq](./taskq) | 异步任务队列，基于 asynq，生产者/消费者模式 |
| [storage](./storage) | 统一对象存储接口，支持阿里云 OSS、腾讯云 COS 和七牛云 KODO。 |
| [websocket](./websocket) | WebSocket 服务封装，基于 gorilla/websocket，事件驱动、房间广播、心跳检测 |
| [stringx](./stringx) | 字符串工具包，随机生成、判断、转换、拆分连接等常用函数 |

## 特性

- **统一风格**：所有模块使用中文注释、英文错误信息、函数式选项配置
- **零依赖侵入**：每个模块独立 import，按需使用
- **类型安全**：广泛使用泛型（`Response[T]`、`cast.To[T]`）
- **可测试**：每个模块都有完整的单元测试，支持 `-race` 检测
- **最新依赖**：使用最新版本的 Go 和第三方库
