package trace

import (
	"net/http"
	"path"
	"strings"

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
// 命中规则的请求直接放行、不创建 span。支持精确匹配与 * 通配符：
//   - "/health"      精确匹配 /health
//   - "/health*"     前缀匹配（/health、/healthz、/health/live）
//   - "/metrics/*"   匹配 /metrics 下的一级路径（* 不跨目录）
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
	matcher := newPathMatcher(ignorePaths)
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
			rec := newStatusRecorder(w)
			next.ServeHTTP(rec, r)
			span.SetAttributes(semconv.HTTPAttributesFromHTTPStatusCode(rec.status)...)
			span.SetStatus(semconv.SpanStatusFromHTTPStatusCodeAndSpanKind(
				rec.status, oteltrace.SpanKindServer))
		})
	}
}

// statusRecorder 包装 http.ResponseWriter，捕获响应状态码。
// 与 httpx 内的实现独立，避免 trace 包依赖 httpx。
type statusRecorder struct {
	http.ResponseWriter
	status    int
	wroteHead bool
}

func newStatusRecorder(w http.ResponseWriter) *statusRecorder {
	return &statusRecorder{ResponseWriter: w, status: http.StatusOK}
}

// WriteHeader 记录状态码（仅首次有效），并委托给底层 ResponseWriter。
func (r *statusRecorder) WriteHeader(code int) {
	if !r.wroteHead {
		r.status = code
		r.wroteHead = true
	}
	r.ResponseWriter.WriteHeader(code)
}

// pathMatcher 判断请求路径是否命中 ignorePaths 忽略规则。
type pathMatcher struct {
	patterns []string
}

func newPathMatcher(patterns []string) *pathMatcher {
	return &pathMatcher{patterns: patterns}
}

// Match 返回 reqPath 是否命中任意忽略规则。
// 支持精确匹配、以 * 结尾的前缀匹配，以及 path.Match 通配符（* 不跨目录）。
func (m *pathMatcher) Match(reqPath string) bool {
	for _, p := range m.patterns {
		if p == "" {
			continue
		}
		if p == reqPath {
			return true
		}
		// 以 * 结尾：前缀匹配（含跨目录子路径），如 /health* 匹配 /healthz、/health/live
		if strings.HasSuffix(p, "*") && strings.HasPrefix(reqPath, strings.TrimSuffix(p, "*")) {
			return true
		}
		// 通配符匹配（* 不跨 /），如 /metrics/* 匹配 /metrics/foo
		if ok, _ := path.Match(p, reqPath); ok {
			return true
		}
	}
	return false
}
