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
}

// NewCORS 创建 CORS 中间件。
// allowOrigins 为允许的来源列表；传入 "*" 表示允许所有来源（优先于其它条目）。
func NewCORS(allowOrigins ...string) *CORS {
	c := &CORS{allowed: make(map[string]struct{}, len(allowOrigins))}
	for _, o := range allowOrigins {
		if o == corsAllowAll {
			c.allowAll = true
			break
		}
		c.allowed[o] = struct{}{}
	}
	return c
}

// Middleware 返回标准形式 func(http.Handler) http.Handler 的 CORS 中间件。
//
// 同源请求（Origin 与 Host 一致）不设置 CORS 头；未授权来源返回 403；
// OPTIONS 预检请求返回 204。
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
					w.WriteHeader(http.StatusForbidden)
					return
				}
			}

			// 设置 CORS 头
			if c.allowAll {
				w.Header().Set(corsAllowOrigin, corsAllowAll)
			} else {
				w.Header().Set(corsAllowOrigin, origin)
				w.Header().Set(corsHeaderVary, corsHeaderOrigin)
			}
			w.Header().Set(corsAllowMethods, corsDefaultMethods)
			w.Header().Set(corsAllowHeaders, corsDefaultHeaders)
			w.Header().Set(corsExposeHeaders, corsDefaultExpose)
			w.Header().Set(corsAllowCredential, corsDefaultCreds)
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
