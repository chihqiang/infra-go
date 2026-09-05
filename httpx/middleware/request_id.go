package middleware

import (
	"context"
	"net/http"

	"github.com/google/uuid"
)

// HeaderRequestID 请求 ID 使用的 HTTP Header 名。
const HeaderRequestID = "X-Request-Id"

// requestIDKey request_id 的 context key 类型（私有类型，避免与其他库冲突）。
type requestIDKey struct{}

// ContextWithRequestID 将 request_id 注入 context。
// httpx 主包的 httpx.ContextWithRequestID 委托本实现，保证响应中的 request_id
// 与 RequestID 中间件注入的 id 读取自同一 key。
func ContextWithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

// RequestIDFromContext 从 context 提取 request_id，不存在时返回空字符串。
func RequestIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if id, ok := ctx.Value(requestIDKey{}).(string); ok {
		return id
	}
	return ""
}

// RequestID 是 request_id 中间件。
type RequestID struct{}

// NewRequestID 创建 RequestID 中间件。
func NewRequestID() *RequestID {
	return &RequestID{}
}

// Middleware 返回标准形式 func(http.Handler) http.Handler 的 request_id 中间件。
// 从 X-Request-Id 请求头读取，不存在则自动生成（google/uuid），
// 注入 context 并回写响应头 X-Request-Id。
func (m *RequestID) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get(HeaderRequestID)
			if id == "" {
				id = uuid.NewString()
			}
			w.Header().Set(HeaderRequestID, id)
			next.ServeHTTP(w, r.WithContext(ContextWithRequestID(r.Context(), id)))
		})
	}
}
