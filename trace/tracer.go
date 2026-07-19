package trace

import (
	"context"
	"net/http"

	"github.com/chihqiang/infra-go/logger"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/metadata"
)

func init() {
	logger.RegisterContextExtractor(func(ctx context.Context) []logger.Field {
		sc := trace.SpanContextFromContext(ctx)
		if !sc.IsValid() {
			return nil
		}
		return []logger.Field{
			logger.String("trace_id", sc.TraceID().String()),
			logger.String("span_id", sc.SpanID().String()),
		}
	})
}

// TraceIDKey HTTP 头中的 trace id 键名。
// https://www.w3.org/TR/trace-context/#trace-id
var TraceIDKey = http.CanonicalHeaderKey("x-trace-id")

// --- gRPC 元数据传播 ---

// metadataSupplier 实现 propagation.TextMapCarrier 接口，
// 用于在 gRPC metadata 中注入和提取链路上下文。
type metadataSupplier struct {
	metadata *metadata.MD
}

func (s *metadataSupplier) Get(key string) string {
	values := s.metadata.Get(key)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func (s *metadataSupplier) Set(key, value string) {
	s.metadata.Set(key, value)
}

func (s *metadataSupplier) Keys() []string {
	out := make([]string, 0, len(*s.metadata))
	for key := range *s.metadata {
		out = append(out, key)
	}
	return out
}

// Inject 将链路上下文注入到 gRPC metadata 中。
// 用于 gRPC 客户端发起请求时，将当前 span 上下文传递到服务端。
func Inject(ctx context.Context, metadata *metadata.MD) {
	otel.GetTextMapPropagator().Inject(ctx, &metadataSupplier{
		metadata: metadata,
	})
}

// Extract 从 gRPC metadata 中提取链路上下文。
// 用于 gRPC 服务端接收请求时，恢复客户端传递的 span 上下文。
func Extract(ctx context.Context, metadata *metadata.MD) (context.Context, trace.SpanContext) {
	ctx = otel.GetTextMapPropagator().Extract(ctx, &metadataSupplier{
		metadata: metadata,
	})
	return ctx, trace.SpanContextFromContext(ctx)
}

// --- HTTP 头传播 ---

// InjectHeader 将链路上下文注入到 HTTP Header 中。
// 用于 HTTP 客户端发起请求时，将当前 span 上下文传递到服务端。
func InjectHeader(ctx context.Context, header http.Header) {
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(header))
}

// ExtractHeader 从 HTTP Header 中提取链路上下文。
// 用于 HTTP 服务端接收请求时，恢复客户端传递的 span 上下文。
func ExtractHeader(ctx context.Context, header http.Header) (context.Context, trace.SpanContext) {
	ctx = otel.GetTextMapPropagator().Extract(ctx, propagation.HeaderCarrier(header))
	return ctx, trace.SpanContextFromContext(ctx)
}

// --- 辅助函数 ---

// ContextWithSpanContext 将 SpanContext 注入到 context 中。
// 用于跨上下文传递链路信息，例如将根 span 的链路注入到 HTTP 请求上下文。
func ContextWithSpanContext(ctx context.Context, sc trace.SpanContext) context.Context {
	return trace.ContextWithSpanContext(ctx, sc)
}

// SpanContextFromContext 从 context 中提取 SpanContext。
func SpanContextFromContext(ctx context.Context) trace.SpanContext {
	return trace.SpanContextFromContext(ctx)
}

// TracerFromContext 从 context 中获取 tracer。
// 如果 context 中有有效的 span，使用其 TracerProvider；否则使用全局 TracerProvider。
func TracerFromContext(ctx context.Context) trace.Tracer {
	if span := trace.SpanFromContext(ctx); span.SpanContext().IsValid() {
		return span.TracerProvider().Tracer(TraceName)
	}
	return otel.Tracer(TraceName)
}

// TraceIDFromContext 返回 context 中的 trace id。
// 如果 context 中没有有效的 span，返回空字符串。
func TraceIDFromContext(ctx context.Context) string {
	sc := trace.SpanContextFromContext(ctx)
	if sc.HasTraceID() {
		return sc.TraceID().String()
	}
	return ""
}

// SpanIDFromContext 返回 context 中的 span id。
// 如果 context 中没有有效的 span，返回空字符串。
func SpanIDFromContext(ctx context.Context) string {
	sc := trace.SpanContextFromContext(ctx)
	if sc.HasSpanID() {
		return sc.SpanID().String()
	}
	return ""
}

// StartSpan 创建并启动一个新的 span。
// 返回带有 span 的 context 和 span 本身。
// 用法：
//
//	ctx, span := trace.StartSpan(ctx, "operation-name",
//	    trace.WithAttributes(trace.AttrString("key", "val")),
//	)
//	defer span.End()
func StartSpan(ctx context.Context, name string, opts ...SpanOption) (context.Context, trace.Span) {
	return TracerFromContext(ctx).Start(ctx, name, opts...)
}
