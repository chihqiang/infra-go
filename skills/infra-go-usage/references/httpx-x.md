# httpx/x

`httpx` 的通用 HTTP 小工具集合，位于 `httpx/x` 子包，供 `httpx/middleware` 各中间件及业务侧复用。与中间件本体无关的零散工具按需收敛于此包，避免为单一函数各自建包。

当前包含：

- **路径匹配器 `PathMatcher`**：中间件 skip / ignore 规则（`WithLogger` / `WithCryption` / `WithRateLimit` / `WithTracing`）的统一实现；
- **客户端 IP 解析 `ClientIP` / `IPChecker`**：获取经反向代理转发后的真实客户端 IP。

```go
import "github.com/chihqiang/infra-go/httpx/x"
```

## PathMatcher 路径匹配

```go
m := x.NewPathMatcher([]string{"/health", "/health*", "/api/*/x"})
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

`httpx.With*` 中间件入口通常已接受 `...string` 路径参数并在内部调用 `x.NewPathMatcher`，业务侧无需直接使用本包；如需自定义匹配规则（如网关鉴权白名单）可 `NewPathMatcher` + `Match` 直接使用。

## ClientIP 客户端 IP 解析

面向「部署于可信反向代理之后」的服务，获取真实客户端 IP（纯 IP，不含端口），常见代理场景（Nginx / CDN / 云 LB）可直接使用。

**主包便捷入口（推荐，业务最常用）**：`httpx` 主包已在 `httpx/request.go` 转发，直接 `httpx.ClientIP(r)` 即可，无需额外 import 子包：

```go
import "github.com/chihqiang/infra-go/httpx"

ip := httpx.ClientIP(r)                          // 默认：回环/私网视为可信代理
ip := httpx.ClientIPWithTrustedProxies(r, "100.64.0.0/10") // 追加可信网段（云 LB/CGNAT）
```

子包入口（`x.ClientIP` / `x.NewIPChecker`，middleware 内部即用它）供需要复用解析器或直接引用子包的场景：

```go
ip := x.ClientIP(r) // 便捷：默认回环/私网视为可信代理
```

安全解析思路：先看直连对端 `RemoteAddr`——不可信（公网直连客户端）则代理头一律忽略，只返回对端；可信（回环/私网/网关）才解析代理头。代理头顺序：厂商头 → `X-Forwarded-For`（从右往左跳过可信代理，防伪造前缀）→ `Forwarded`（RFC 7239）→ `X-Real-IP` → 回退 `RemoteAddr`。

需要自定义（流量经公网 CDN/WAF 回源、启用 Cloudflare 等厂商头）时用可复用解析器：

```go
ipc := x.NewIPChecker(
    x.WithTrustedProxies("100.64.0.0/10"),     // 追加可信代理网段
    x.WithVendorHeaders(x.HeaderCFConnectingIP), // 启用 Cloudflare 头
)
ip := ipc.ClientIP(r)
```

常用头常量：`HeaderXForwardedFor`、`HeaderXRealIP`、`HeaderForwarded`、`HeaderCFConnectingIP`、`HeaderTrueClientIP`。详见 `x` 包源码注释（解析优先级与信任模型）。
