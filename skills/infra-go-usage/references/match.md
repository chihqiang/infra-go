# httpx/match

轻量路径匹配器，位于 `httpx/match` 子包，供 `httpx/middleware` 各中间件复用同一套「忽略 / 跳过」规则，避免各自重复实现。

```go
import "github.com/chihqiang/infra-go/httpx/match"
```

## PathMatcher

```go
m := match.NewPathMatcher([]string{"/health", "/health*", "/api/*/x"})
m.Match("/healthz") // true
```

### 规则语义

每条规则支持三种形式：

| 形式 | 示例 | 说明 |
|------|------|------|
| 精确匹配 | `/health` | 仅命中该路径 |
| 前缀通配 | `/health*` | 以 `*` 结尾，命中以该前缀开头的路径（**可跨目录**，`/health/live` 也命中） |
| glob 通配 | `/api/*/x`、`/v[0-9]/info` | 基于 `path.Match`，`*` **不跨目录**，支持 `?`、`[...]` |

> **注意**：以 `*` 结尾的规则按「前缀」匹配（可跨目录），因此 `/metrics/*` 也会命中 `/metrics/a/b`；若只需匹配一级子路径，请使用不含尾 `*` 的 glob 规则，如 `/metrics/?` 或 `/*/foo`。

空字符串规则会被忽略；未传规则时不命中任何路径。

## 应用场景

```go
// 经 httpx.With* 中间件跳过特定路径（中间件内部已调用 NewPathMatcher，无需直接使用本包）
server.Use(httpx.WithLogger("/health*", "/metrics/*"))   // 访问日志跳过探活
server.Use(httpx.WithCryption(key, "/callback", "/public/*")) // 加解密跳过
server.Use(httpx.WithRateLimit(limiter, "/healthz"))     // 限流跳过探活
server.Use(httpx.WithTracing("/health*", "/metrics/*"))  // 链路追踪跳过探活
```

`httpx.With*` 中间件入口通常已接受 `...string` 路径参数并在内部调用 `match.NewPathMatcher`，业务侧无需直接使用本包；如需自定义匹配规则（如网关鉴权白名单）可 `NewPathMatcher` + `Match` 直接使用。
