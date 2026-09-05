package middleware

import (
	"context"
	"net/http"

	"github.com/chihqiang/infra-go/httpx/match"
	"github.com/chihqiang/infra-go/httpx/respw"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// defaultTracerName 追踪器名称，与 trace 包 TraceName 保持一致，
// 使经本中间件创建的 span 与 trace.StartSpan 等属于同一 tracer。
const defaultTracerName = "infra-go"

// Tracing 是 HTTP 服务端链路追踪中间件。
//
// 功能：
//   - 从请求头提取上游传播的 span 上下文（W3C traceparent）
//   - 为每个请求创建服务端 span（携带 method/path/status 等 HTTP 语义属性）
//   - 将 span 注入 context，供下游 logger/orm/redisx 等模块自动关联 trace_id
//
// 默认使用全局 TracerProvider（经 trace.StartAgent 装配）；请求 context 中若
// 已有有效 span，则沿用其 TracerProvider（支持链路内嵌套追踪）。
type Tracing struct {
	matcher *match.PathMatcher
	name    string
}

// NewTracing 创建 HTTP 服务端链路追踪中间件。
// ignorePaths 用于指定不追踪的请求路径（如健康检查、探针、监控等），命中规则的
// 请求直接放行、不创建 span。路径匹配由 match 包统一提供，支持三种形式（详见
// match.NewPathMatcher）：
//   - 精确匹配：如 "/health"
//   - 前缀通配：以 "*" 结尾且可跨目录，如 "/health*" 命中 /health、/healthz、/health/live
//   - glob 通配：* 不跨目录，如 "/api/*/x"
func NewTracing(ignorePaths ...string) *Tracing {
	return &Tracing{
		matcher: match.NewPathMatcher(ignorePaths),
		name:    defaultTracerName,
	}
}

// WithTracerName 覆盖默认追踪器名称（默认 "infra-go"，与 trace.TraceName 一致）；
// name 为空时保持默认。
func (t *Tracing) WithTracerName(name string) *Tracing {
	if name != "" {
		t.name = name
	}
	return t
}

// Middleware 返回标准形式 func(http.Handler) http.Handler 的链路追踪中间件，
// 不依赖具体 HTTP 框架，可用于标准 net/http、httpx、gin、echo 等：
//
//	// 标准 net/http
//	handler := middleware.NewTracing("/health*", "/metrics/*").Middleware()(mux)
//
//	// httpx（httpx.WithTracing 已内置便捷函数，直接 server.Use 即可）
//	server.Use(httpx.WithTracing("/health*"))
func (t *Tracing) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 命中忽略列表的路径直接放行，不创建 span
			if t.matcher.Match(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			// 提取上游传播的链路上下文
			ctx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))
			spanName := r.URL.Path + " " + r.Method

			ctx, span := t.tracer(ctx).Start(ctx, spanName,
				oteltrace.WithSpanKind(oteltrace.SpanKindServer),
				oteltrace.WithAttributes(
					semconv.HTTPServerAttributesFromHTTPRequest("", spanName, r)...,
				),
			)
			defer span.End()

			// 注入 span 上下文，下游模块可通过 trace.TraceIDFromContext 关联
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

// tracer 返回当前请求应使用的 tracer：
// context 中存在有效 span 时使用其 TracerProvider 的 tracer（支持嵌套追踪），
// 否则使用全局 TracerProvider 的 tracer。
func (t *Tracing) tracer(ctx context.Context) oteltrace.Tracer {
	if span := oteltrace.SpanFromContext(ctx); span.SpanContext().IsValid() {
		return span.TracerProvider().Tracer(t.name)
	}
	return otel.Tracer(t.name)
}
