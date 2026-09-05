package httpx

import (
	"context"

	"github.com/chihqiang/infra-go/httpx/middleware"
	"github.com/chihqiang/infra-go/logger"
)

// 本文件提供 request_id 的 context 工具：
// ContextWithRequestID / RequestIDFromContext，并在 init 中接入 logger 的 context 字段提取。
//
// request_id 的 context key 与存取实现统一放在 httpx/middleware 子包（RequestID 中间件
// 注入、统一 JSON 响应读取、本包便捷函数共享同一 key），本文件仅做委托转发并接入 logger。

func init() {
	logger.RegisterContextExtractor(func(ctx context.Context) []logger.Field {
		ri := RequestIDFromContext(ctx)
		if ri == "" {
			return nil
		}
		return []logger.Field{
			logger.String("request_id", ri),
		}
	})
}

// ContextWithRequestID 将 request_id 注入 context。
// 配合 RequestIDFromContext 使用：
//
//	ctx := httpx.ContextWithRequestID(r.Context(), "req-123")
//	resp := httpx.OkJSONCtx(ctx, w, data)
func ContextWithRequestID(ctx context.Context, id string) context.Context {
	return middleware.ContextWithRequestID(ctx, id)
}

// RequestIDFromContext 从 context 提取 request_id，不存在时返回空字符串。
func RequestIDFromContext(ctx context.Context) string {
	return middleware.RequestIDFromContext(ctx)
}
