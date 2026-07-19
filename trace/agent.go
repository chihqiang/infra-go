package trace

import (
	"context"
	"fmt"
	"sync"

	"github.com/chihqiang/infra-go/logger"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/exporters/zipkin"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

var (
	once           sync.Once
	tp             *sdktrace.TracerProvider
	shutdownOnceFn = sync.OnceFunc(func() {
		if tp != nil {
			_ = tp.Shutdown(context.Background())
		}
	})
)

// StartAgent 启动链路追踪 agent。
// 使用 sync.Once 确保只初始化一次，多次调用安全。
func StartAgent(cfg Config) {
	c := fillDefault(cfg)

	if c.Disabled {
		return
	}

	once.Do(func() {
		if err := startAgent(c); err != nil {
			logger.Error(fmt.Sprintf("trace agent: %v", err))
		}
	})
}

// StopAgent 关闭链路追踪 agent，刷新未导出的 span。
// 通常在程序退出前调用。
func StopAgent() {
	shutdownOnceFn()
}

// startAgent 启动 agent 的内部实现。
func startAgent(c Config) error {
	// 添加服务名资源属性
	AddResources(semconv.ServiceNameKey.String(c.Name))

	opts := []sdktrace.TracerProviderOption{
		// 基于父 span 的采样率设置
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(c.Sampler))),
		// 记录应用信息到 Resource
		sdktrace.WithResource(resource.NewSchemaless(attrResources...)),
	}

	// 配置导出器
	if len(c.Endpoint) > 0 {
		exp, err := createExporter(c)
		if err != nil {
			return fmt.Errorf("failed to create trace exporter: %w", err)
		}
		// 生产环境使用批量导出
		opts = append(opts, sdktrace.WithBatcher(exp))
	}

	tp = sdktrace.NewTracerProvider(opts...)

	// 设置全局 TracerProvider
	otel.SetTracerProvider(tp)

	// 设置错误处理器，将 otel 内部错误转发到 logger
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		logger.Error(fmt.Sprintf("[otel] error: %v", err))
	}))

	return nil
}

// createExporter 根据配置创建对应的 span 导出器。
func createExporter(c Config) (sdktrace.SpanExporter, error) {
	switch c.Batcher {
	case BatcherZipkin:
		return zipkin.New(c.Endpoint)

	case BatcherOTLPGRPC:
		// 使用非阻塞模式，避免导出器不可达时拖慢应用启动
		opts := []otlptracegrpc.Option{
			otlptracegrpc.WithInsecure(),
			otlptracegrpc.WithEndpoint(c.Endpoint),
		}
		if len(c.OtlpHeaders) > 0 {
			opts = append(opts, otlptracegrpc.WithHeaders(c.OtlpHeaders))
		}
		return otlptracegrpc.New(context.Background(), opts...)

	case BatcherOTLPHTTP:
		opts := []otlptracehttp.Option{
			otlptracehttp.WithEndpoint(c.Endpoint),
		}
		if !c.OtlpHttpSecure {
			opts = append(opts, otlptracehttp.WithInsecure())
		}
		if len(c.OtlpHeaders) > 0 {
			opts = append(opts, otlptracehttp.WithHeaders(c.OtlpHeaders))
		}
		if len(c.OtlpHttpPath) > 0 {
			opts = append(opts, otlptracehttp.WithURLPath(c.OtlpHttpPath))
		}
		return otlptracehttp.New(context.Background(), opts...)

	case BatcherFile:
		f, err := openFileForExporter(c.Endpoint)
		if err != nil {
			return nil, fmt.Errorf("file exporter endpoint error: %w", err)
		}
		return stdouttrace.New(stdouttrace.WithWriter(f))

	default:
		return nil, fmt.Errorf("unsupported batcher type: %s", c.Batcher)
	}
}
