package middleware

import "net/http"

// CORS 响应头常量（仅在 CORS 逻辑内部使用，直接内联于此文件）。
const (
	corsAllowAll        = "*"
	corsHeaderOrigin    = "Origin"
	corsHeaderVary      = "Vary"
	corsAllowOrigin     = "Access-Control-Allow-Origin"
	corsAllowMethods    = "Access-Control-Allow-Methods"
	corsAllowHeaders    = "Access-Control-Allow-Headers"
	corsExposeHeaders   = "Access-Control-Expose-Headers"
	corsAllowCredential = "Access-Control-Allow-Credentials"
	corsMaxAge          = "Access-Control-Max-Age"
	corsDefaultMethods  = "GET, POST, PUT, DELETE, OPTIONS, PATCH"
	corsDefaultHeaders  = "Content-Type, Authorization, X-Requested-With, Accept, Origin"
	corsDefaultExpose   = "Content-Length, Content-Type"
	corsDefaultCreds    = "true"
	corsDefaultMaxAge   = "86400"
	schemeHTTP          = "http"
	schemeHTTPS         = "https"
)

// CORS 为响应设置 CORS 头的中间件。
// 构造时预构建允许来源集合，避免每次请求 O(n) 扫描。
type CORS struct {
	allowAll bool
	allowed  map[string]struct{}
	// credentials 是否下发 Access-Control-Allow-Credentials: true，默认 true。
	credentials bool
	// rejectUnauthorized 未授权来源是否直接返回 403，默认 false（透传）。
	rejectUnauthorized bool
}

// NewCORS 创建 CORS 中间件。
// allowOrigins 为允许的来源列表；传入 "*" 表示允许所有来源（优先于其它条目）。
//
// 关于凭证（Access-Control-Allow-Credentials）：
// 按 Fetch 规范，"允许所有来源"与"允许携带凭证"不能同时用通配符表达 ——
// 浏览器会拒绝 `Access-Control-Allow-Origin: *` 与
// `Access-Control-Allow-Credentials: true` 的组合。因此 allowAll 模式下
// 本中间件回显具体 Origin（并附带 Vary: Origin），而不是发送 "*"。
//
// ⚠️ 安全提示：allowAll + 凭证意味着**任意站点**都能发起带凭证的跨域请求
// 并读取响应。生产环境请改用显式来源列表，或在确定不需要 cookie/Authorization
// 时用 WithCredentials(false) 关闭凭证。
//
// 未授权来源的默认处理是**透传**（不下发 CORS 头，请求继续交给下游），
// 详见 WithRejectUnauthorizedOrigin。
func NewCORS(allowOrigins ...string) *CORS {
	c := &CORS{
		allowed:     make(map[string]struct{}, len(allowOrigins)),
		credentials: true,
	}
	for _, o := range allowOrigins {
		if o == corsAllowAll {
			c.allowAll = true
			break
		}
		c.allowed[o] = struct{}{}
	}
	return c
}

// WithCredentials 设置是否下发 Access-Control-Allow-Credentials。
//
// 默认 true（保持既有行为）。设为 false 时不下发该响应头，
// 浏览器将禁止跨域请求携带 cookie / TLS 客户端证书，
// 并把 Authorization 头排除在可暴露范围之外 —— 适用于纯公开 API。
func (c *CORS) WithCredentials(enable bool) *CORS {
	c.credentials = enable
	return c
}

// WithRejectUnauthorizedOrigin 设置未授权来源是否直接返回 403，默认 false。
//
// **默认透传（false）**：不下发 CORS 头，请求继续交给下游 handler。
// 这是 CORS 的标准做法，原因是：
//
//   - CORS 是**浏览器侧**的响应读取限制，而不是服务端的请求准入控制。
//     缺少 Access-Control-Allow-Origin 时，浏览器已经会阻止脚本读取响应，
//     服务端不需要（也不应该）再拒绝一次。
//   - 返回 403 会误伤**非浏览器客户端**：curl、移动端、服务间调用、
//     以及部分 HTTP 库会无条件带上 Origin 头，它们不受 CORS 约束，
//     却会因为 403 被挡在业务逻辑之外。
//   - 预检（OPTIONS）本就无法通过，浏览器不会发出真实请求；
//     真实请求若被放行，其响应也不可被跨域脚本读取，因此透传并无安全损失。
//
// 设为 true 时恢复"未授权来源 → 403"的严格行为，语义是
// "只服务指定来源"，而不是"浏览器不能读"。请按实际需要选择：
// 若业务确实只面向白名单来源（例如纯前端应用的后端），严格模式可提前拒绝、
// 省去无谓的下游处理；但**不要**把 CORS 当作 CSRF 防护 ——
// 简单请求（表单 POST、img GET）本就不受 CORS 阻止，
// 状态变更接口请另行使用 CSRF token 或 SameSite Cookie。
func (c *CORS) WithRejectUnauthorizedOrigin(reject bool) *CORS {
	c.rejectUnauthorized = reject
	return c
}

// Middleware 返回标准形式 func(http.Handler) http.Handler 的 CORS 中间件。
//
// 同源请求（Origin 与 Host 一致）不设置 CORS 头；授权来源下发 CORS 头，
// OPTIONS 预检返回 204；未授权来源默认透传（不下发 CORS 头），
// 可用 WithRejectUnauthorizedOrigin(true) 改为返回 403。
func (c *CORS) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get(corsHeaderOrigin)
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}

			// 同源请求不需要 CORS 头
			scheme := schemeHTTP
			if r.TLS != nil {
				scheme = schemeHTTPS
			}
			if origin == scheme+"://"+r.Host {
				next.ServeHTTP(w, r)
				return
			}

			// 校验 Origin
			if !c.allowAll {
				if _, ok := c.allowed[origin]; !ok {
					if c.rejectUnauthorized {
						w.WriteHeader(http.StatusForbidden)
						return
					}
					// 透传：不下发任何 CORS 头，浏览器会阻止脚本读取响应。
					// 非浏览器客户端不受影响。
					next.ServeHTTP(w, r)
					return
				}
			}

			// 回显具体 Origin：响应随 Origin 变化，必须声明 Vary 以免被缓存串用。
			//
			// allowAll 模式下**不再**发送 "*"：与 Allow-Credentials: true 共用时
			// 浏览器会拒绝整个响应，导致带凭证的跨域请求（withCredentials /
			// credentials: 'include'）在旧实现下必然失败。
			// 回显 Origin 同时满足"允许任意来源"与"允许凭证"，是浏览器可接受的唯一形式。
			w.Header().Set(corsAllowOrigin, origin)
			w.Header().Set(corsHeaderVary, corsHeaderOrigin)
			w.Header().Set(corsAllowMethods, corsDefaultMethods)
			w.Header().Set(corsAllowHeaders, corsDefaultHeaders)
			w.Header().Set(corsExposeHeaders, corsDefaultExpose)
			if c.credentials {
				w.Header().Set(corsAllowCredential, corsDefaultCreds)
			}
			w.Header().Set(corsMaxAge, corsDefaultMaxAge)

			// OPTIONS 预检
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
