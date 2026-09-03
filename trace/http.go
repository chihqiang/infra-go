package trace

import (
	"net/http"

	"github.com/chihqiang/infra-go/match"
	"github.com/chihqiang/infra-go/respw"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// HTTPMiddleware 返回一个 HTTP 服务端链路追踪中间件。
// 返回标准形式 func(http.Handler) http.Handler，不依赖具体 HTTP 框架，
// 可用于标准 net/http、httpx、gin、echo 等任何框架。
//
// ignorePaths 用于指定不追踪的请求路径（如健康检查、探针、监控等），
// 命中规则的请求直接放行、不创建 span。路径匹配由 match 包统一提供，
// 支持三种形式（详见 match.NewPathMatcher）：
//   - 精确匹配：如 "/health"
//   - 前缀通配：以 "*" 结尾且可跨目录，如 "/health*" 命中 /health、/healthz、/health/live
//   - glob 通配：* 不跨目录，如 "/api/*/x"
//
// 功能：
//   - 从请求头提取上游传播的 span 上下文（W3C traceparent）
//   - 为每个请求创建服务端 span（携带 method/path/status 等 HTTP 语义属性）
//   - 将 span 注入 context，供下游 logger/orm/redisx 等模块自动关联 trace_id
//
// 用法（标准 net/http）：
//
//	handler := trace.HTTPMiddleware("/health*", "/metrics/*")(mux)
//	http.ListenAndServe(":8080", handler)
//
// 用法（httpx）：
//
//	server.Use(func(next http.HandlerFunc) http.HandlerFunc {
//	    return func(w http.ResponseWriter, r *http.Request) {
//	        trace.HTTPMiddleware()(http.HandlerFunc(next)).ServeHTTP(w, r)
//	    }
//	})
func HTTPMiddleware(ignorePaths ...string) func(http.Handler) http.Handler {
	matcher := match.NewPathMatcher(ignorePaths)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 命中忽略列表的路径直接放行，不创建 span
			if matcher.Match(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			// 提取上游传播的链路上下文
			ctx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))
			spanName := r.URL.Path + " " + r.Method

			ctx, span := TracerFromContext(ctx).Start(ctx, spanName,
				oteltrace.WithSpanKind(oteltrace.SpanKindServer),
				oteltrace.WithAttributes(
					semconv.HTTPServerAttributesFromHTTPRequest("", spanName, r)...,
				),
			)
			defer span.End()

			// 注入 span 上下文，下游模块可通过 TraceIDFromContext 关联
			r = r.WithContext(ctx)

			// 记录响应状态码到 span
			rec := respw.NewRecorderWriter(w)
			next.ServeHTTP(rec, r)
			span.SetAttributes(semconv.HTTPAttributesFromHTTPStatusCode(rec.Status())...)
			span.SetStatus(semconv.SpanStatusFromHTTPStatusCodeAndSpanKind(
				rec.Status(), oteltrace.SpanKindServer))
		})
	}
}
