# match

轻量路径/模式匹配器，供各 HTTP 中间件复用同一套「忽略/跳过规则」，避免各自重复实现（如 `httpx.WithLogger` / `httpx.WithCryption` 的 `skipPaths`、`trace.HTTPMiddleware` 的 `ignorePaths`）。

```go
import "github.com/chihqiang/infra-go/match"
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

### 应用场景

```go
// httpx：跳过访问日志 / 请求-响应加解密
server.Use(httpx.WithLogger("/health*", "/metrics/*"))
server.Use(httpx.WithCryption(key, "/callback", "/public/*"))

// trace：忽略链路追踪
handler := trace.HTTPMiddleware("/health*", "/metrics/*")(mux)
```

各中间件入口通常已接受 `...string` 路径参数并内部调用 `NewPathMatcher`，业务侧无需直接使用本包；如需自定义匹配规则（如网关鉴权白名单）可直接使用 `NewPathMatcher` + `Match`。
