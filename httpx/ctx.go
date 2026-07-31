package httpx

import "context"

// requestIDKey request_id 的 context key 类型（私有类型，避免与其他库冲突）。
type requestIDKey struct{}

// ContextWithRequestID 将 request_id 注入 context。
// 配合 RequestIDFromContext 使用：
//
//	ctx := httpx.ContextWithRequestID(r.Context(), "req-123")
//	resp := httpx.OkJSONCtx(ctx, w, data)
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
