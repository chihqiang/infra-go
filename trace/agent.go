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

// agentState 表示一次 agent 生命周期的资源。
type agentState struct {
	tp *sdktrace.TracerProvider
	// closers 需要在 StopAgent 时释放（如 file 导出器的文件句柄）。
	// 由 agent 自己持有，而不是存在包级变量中——后者在多实例/重复启动时会互相覆盖，
	// 导致先前打开的文件永远无法关闭（句柄泄漏）。
	closers []func() error
}

var (
	// agentLk 保护 currentAgent，使 StartAgent/StopAgent 可安全并发调用。
	agentLk      sync.Mutex
	currentAgent *agentState
)

// StartAgent 启动链路追踪 agent。
//
// 重复调用时行为如下：
//   - 已有一个运行中的 agent（未调用 StopAgent）→ 忽略本次配置并记录警告。
//     配置在 agent 启动时快照，无法热更新；需要换配置请先 StopAgent 再 StartAgent。
//   - 已调用过 StopAgent → 允许重新启动（使用新配置）。
//
// opts 用于表达 Config 结构体无法表达的显式零值（如 Sampler = 0），见 Option。
//
// 旧实现用 sync.Once 实现“只初始化一次”，副作用是 StopAgent 之后再也无法重启，
// 而 otel 仍指向已关闭的 TracerProvider，导致后续 span 被静默丢弃。
func StartAgent(cfg Config, opts ...Option) {
	c := fillDefault(cfg, opts...)

	if c.Disabled {
		return
	}

	agentLk.Lock()
	defer agentLk.Unlock()

	if currentAgent != nil {
		logger.Warn("trace agent: already started, ignoring new config; call StopAgent first to reload")
		return
	}

	st, err := startAgent(c)
	if err != nil {
		logger.Error(fmt.Sprintf("trace agent: %v", err))
		return
	}
	currentAgent = st
}

// StopAgent 关闭链路追踪 agent，刷新未导出的 span，并释放导出器占用的句柄。
// 通常在程序退出前调用；可重复调用（幂等），也可在之后重新 StartAgent。
func StopAgent() {
	agentLk.Lock()
	defer agentLk.Unlock()

	st := currentAgent
	if st == nil {
		return
	}
	currentAgent = nil

	if st.tp != nil {
		_ = st.tp.Shutdown(context.Background())
	}
	// 关闭文件等资源，避免句柄泄漏
	for _, closeFn := range st.closers {
		if closeFn != nil {
			_ = closeFn()
		}
	}
}

// startAgent 启动 agent 的内部实现，返回本次生命周期的状态。
func startAgent(c Config) (*agentState, error) {
	// 添加服务名资源属性
	AddResources(semconv.ServiceNameKey.String(c.Name))

	opts := []sdktrace.TracerProviderOption{
		// 基于父 span 的采样率设置
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(c.Sampler))),
		// 记录应用信息到 Resource（快照当前属性，之后新增的不再生效）
		sdktrace.WithResource(resource.NewSchemaless(resourceAttrs()...)),
	}

	st := &agentState{}

	// 配置导出器
	if len(c.Endpoint) > 0 {
		exp, closers, err := createExporterWithClosers(c)
		if err != nil {
			return nil, fmt.Errorf("failed to create trace exporter: %w", err)
		}
		st.closers = closers
		// 生产环境使用批量导出
		opts = append(opts, sdktrace.WithBatcher(exp))
	}

	st.tp = sdktrace.NewTracerProvider(opts...)

	// 设置全局 TracerProvider
	otel.SetTracerProvider(st.tp)

	// 设置错误处理器，将 otel 内部错误转发到 logger
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		logger.Error(fmt.Sprintf("[otel] error: %v", err))
	}))

	return st, nil
}

// createExporter 根据配置创建对应的 span 导出器。
func createExporter(c Config) (sdktrace.SpanExporter, error) {
	exp, _, err := createExporterWithClosers(c)
	return exp, err
}

// createExporterWithClosers 创建导出器，并返回需要随 agent 生命周期释放的关闭函数。
func createExporterWithClosers(c Config) (sdktrace.SpanExporter, []func() error, error) {
	switch c.Batcher {
	case BatcherZipkin:
		exp, err := zipkin.New(c.Endpoint)
		return exp, nil, err

	case BatcherOTLPGRPC:
		// 使用非阻塞模式，避免导出器不可达时拖慢应用启动
		opts := []otlptracegrpc.Option{
			otlptracegrpc.WithEndpoint(c.Endpoint),
		}
		if !c.OtlpGrpcSecure {
			opts = append(opts, otlptracegrpc.WithInsecure())
		}
		if len(c.OtlpHeaders) > 0 {
			opts = append(opts, otlptracegrpc.WithHeaders(c.OtlpHeaders))
		}
		exp, err := otlptracegrpc.New(context.Background(), opts...)
		return exp, nil, err

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
		exp, err := otlptracehttp.New(context.Background(), opts...)
		return exp, nil, err

	case BatcherFile:
		f, closeFn, err := openFileForExporter(c.Endpoint)
		if err != nil {
			return nil, nil, fmt.Errorf("file exporter endpoint error: %w", err)
		}
		exp, err := stdouttrace.New(stdouttrace.WithWriter(f))
		if err != nil {
			// 创建失败时立即释放已打开的文件，避免句柄泄漏
			_ = closeFn()
			return nil, nil, err
		}
		return exp, []func() error{closeFn}, nil

	default:
		return nil, nil, fmt.Errorf("unsupported batcher type: %s", c.Batcher)
	}
}
