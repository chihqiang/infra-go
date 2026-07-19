package trace

import (
	"context"
	"net/http"
	"os"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"google.golang.org/grpc/metadata"
)

func TestFillDefault_AllDefaults(t *testing.T) {
	c := fillDefault(Config{})

	assert.Equal(t, "infra-go", c.Name)
	assert.Equal(t, "", c.Endpoint)
	assert.InDelta(t, 1.0, c.Sampler, 0.001)
	assert.Equal(t, BatcherOTLPGRPC, c.Batcher)
	assert.False(t, c.Disabled)
}

func TestFillDefault_UserOverrides(t *testing.T) {
	c := fillDefault(Config{
		Name:      "my-service",
		Endpoint:  "localhost:4317",
		Sampler:   0.5,
		Batcher:   BatcherZipkin,
		Disabled:  true,
		OtlpHeaders: map[string]string{"key": "val"},
		OtlpHttpPath: "/v1/traces",
		OtlpHttpSecure: true,
	})

	assert.Equal(t, "my-service", c.Name)
	assert.Equal(t, "localhost:4317", c.Endpoint)
	assert.InDelta(t, 0.5, c.Sampler, 0.001)
	assert.Equal(t, BatcherZipkin, c.Batcher)
	assert.True(t, c.Disabled)
	assert.Equal(t, "val", c.OtlpHeaders["key"])
	assert.Equal(t, "/v1/traces", c.OtlpHttpPath)
	assert.True(t, c.OtlpHttpSecure)
}

func TestStartAgent_Disabled(t *testing.T) {
	// Disabled 为 true 时不应初始化任何东西
	assert.NotPanics(t, func() {
		StartAgent(Config{Disabled: true})
	})
}

func TestStartAgent_FileExporter(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := tmpDir + "/trace.log"

	// 重置 once 以便重复测试
	resetOnce()

	StartAgent(Config{
		Name:     "test-service",
		Endpoint: logFile,
		Batcher:  BatcherFile,
		Sampler:  1.0,
	})
	defer func() {
		StopAgent()
		closeFile()
		resetOnce()
	}()

	// 创建 span 验证导出器工作
	tracer := otel.Tracer(TraceName)
	ctx, span := tracer.Start(context.Background(), "test-operation")
	span.End()

	// 确保刷新
	require.NoError(t, span.TracerProvider().(interface {
		ForceFlush(context.Context) error
	}).ForceFlush(ctx))

	// 验证文件有内容
	data, err := readFile(logFile)
	require.NoError(t, err)
	assert.NotEmpty(t, data)
}

func TestStartAgent_NoEndpoint(t *testing.T) {
	// 不设置 Endpoint 时，不创建导出器但 tracer provider 仍然初始化
	resetOnce()

	StartAgent(Config{
		Name:    "test-service",
		Sampler: 1.0,
	})
	defer func() {
		StopAgent()
		resetOnce()
	}()

	// 创建 span 不应 panic
	tracer := otel.Tracer(TraceName)
	_, span := tracer.Start(context.Background(), "test-no-export")
	span.End()
}

func TestStopAgent_MultipleCalls(t *testing.T) {
	// 多次调用 StopAgent 不应 panic
	assert.NotPanics(t, func() {
		StopAgent()
		StopAgent()
	})
}

func TestCreateExporter_UnsupportedBatcher(t *testing.T) {
	_, err := createExporter(Config{
		Batcher:  "unsupported",
		Endpoint: "localhost:4317",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported batcher type")
}

func TestCreateExporter_FileError(t *testing.T) {
	_, err := createExporter(Config{
		Batcher:  BatcherFile,
		Endpoint: "/nonexistent_dir/deep/path/trace.log",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "file exporter endpoint error")
}

func TestAddResources(t *testing.T) {
	resetResources()
	defer resetResources()

	AddResources(AttrString("env", "test"))
	AddResources(AttrString("region", "us-east-1"))

	assert.Len(t, attrResources, 2)
	assert.Equal(t, "env", string(attrResources[0].Key))
	assert.Equal(t, "test", attrResources[0].Value.AsString())
}

func TestTraceIDFromContext_Empty(t *testing.T) {
	// 没有 span 的 context 应返回空字符串
	ctx := context.Background()
	assert.Equal(t, "", TraceIDFromContext(ctx))
}

func TestSpanIDFromContext_Empty(t *testing.T) {
	ctx := context.Background()
	assert.Equal(t, "", SpanIDFromContext(ctx))
}

func TestTracerFromContext_Global(t *testing.T) {
	// 没有 span 的 context 应返回全局 tracer
	tracer := TracerFromContext(context.Background())
	assert.NotNil(t, tracer)
}

func TestStartSpan(t *testing.T) {
	resetOnce()
	defer func() {
		StopAgent()
		resetOnce()
	}()

	StartAgent(Config{
		Name:    "test-span",
		Sampler: 1.0,
	})

	ctx := context.Background()
	ctx, span := StartSpan(ctx, "test-operation")
	defer span.End()

	// 启动 span 后 context 中应有 trace id
	traceID := TraceIDFromContext(ctx)
	assert.NotEmpty(t, traceID)

	spanID := SpanIDFromContext(ctx)
	assert.NotEmpty(t, spanID)
}

func TestInjectExtract_GRPC(t *testing.T) {
	resetOnce()
	defer func() {
		StopAgent()
		resetOnce()
	}()

	StartAgent(Config{
		Name:    "test-inject",
		Sampler: 1.0,
	})

	// 创建一个 span
	ctx, span := StartSpan(context.Background(), "parent-op")
	defer span.End()

	// 注入到 gRPC metadata
	md := newGRPCMetadata()
	Inject(ctx, &md)

	// 从 metadata 中提取
	extractedCtx, sc := Extract(context.Background(), &md)
	assert.True(t, sc.IsValid())

	// 提取后的 trace id 应与原始一致
	extractedTraceID := TraceIDFromContext(extractedCtx)
	originalTraceID := TraceIDFromContext(ctx)
	assert.Equal(t, originalTraceID, extractedTraceID)
}

func TestInjectExtract_HTTP(t *testing.T) {
	resetOnce()
	defer func() {
		StopAgent()
		resetOnce()
	}()

	StartAgent(Config{
		Name:    "test-inject-http",
		Sampler: 1.0,
	})

	// 创建一个 span
	ctx, span := StartSpan(context.Background(), "parent-http-op")
	defer span.End()

	// 注入到 HTTP header
	header := http.Header{}
	InjectHeader(ctx, header)

	// 从 header 中提取
	extractedCtx, sc := ExtractHeader(context.Background(), header)
	assert.True(t, sc.IsValid())

	// 提取后的 trace id 应与原始一致
	extractedTraceID := TraceIDFromContext(extractedCtx)
	originalTraceID := TraceIDFromContext(ctx)
	assert.Equal(t, originalTraceID, extractedTraceID)
}

func TestInjectExtract_NoSpan(t *testing.T) {
	// 没有 span 的 context 注入后提取应无有效 span context
	md := newGRPCMetadata()
	Inject(context.Background(), &md)

	_, sc := Extract(context.Background(), &md)
	assert.False(t, sc.IsValid())
}

func TestTraceIDKey(t *testing.T) {
	assert.Equal(t, "X-Trace-Id", TraceIDKey)
}

func TestBatcherConstants(t *testing.T) {
	assert.Equal(t, Batcher("otlpgrpc"), BatcherOTLPGRPC)
	assert.Equal(t, Batcher("otlphttp"), BatcherOTLPHTTP)
	assert.Equal(t, Batcher("zipkin"), BatcherZipkin)
	assert.Equal(t, Batcher("file"), BatcherFile)
}

func TestAttrString(t *testing.T) {
	attr := AttrString("key", "value")
	assert.Equal(t, "key", string(attr.Key))
	assert.Equal(t, "value", attr.Value.AsString())
}

func TestAttrInt(t *testing.T) {
	attr := AttrInt("count", 42)
	assert.Equal(t, "count", string(attr.Key))
	assert.Equal(t, int64(42), attr.Value.AsInt64())
}

func TestAttrInt64(t *testing.T) {
	attr := AttrInt64("id", 9999999999)
	assert.Equal(t, "id", string(attr.Key))
	assert.Equal(t, int64(9999999999), attr.Value.AsInt64())
}

func TestAttrBool(t *testing.T) {
	attr := AttrBool("enabled", true)
	assert.Equal(t, "enabled", string(attr.Key))
	assert.True(t, attr.Value.AsBool())
}

func TestAttrFloat64(t *testing.T) {
	attr := AttrFloat64("ratio", 0.75)
	assert.Equal(t, "ratio", string(attr.Key))
	assert.InDelta(t, 0.75, attr.Value.AsFloat64(), 0.001)
}

func TestAttrStringSlice(t *testing.T) {
	attr := AttrStringSlice("tags", []string{"a", "b"})
	assert.Equal(t, "tags", string(attr.Key))
	assert.Equal(t, []string{"a", "b"}, attr.Value.AsStringSlice())
}

func TestAttrIntSlice(t *testing.T) {
	attr := AttrIntSlice("nums", []int{1, 2, 3})
	assert.Equal(t, "nums", string(attr.Key))
	vals := attr.Value.AsInt64Slice()
	assert.Equal(t, []int64{1, 2, 3}, vals)
}

func TestStartSpan_WithAttributes(t *testing.T) {
	resetOnce()
	defer func() {
		StopAgent()
		resetOnce()
	}()

	StartAgent(Config{
		Name:    "test-attr",
		Sampler: 1.0,
	})

	ctx, span := StartSpan(context.Background(), "op-with-attr",
		WithAttributes(
			AttrString("user", "alice"),
			AttrInt("age", 30),
			AttrBool("vip", true),
		),
	)
	defer span.End()

	// span 应正常创建
	assert.NotNil(t, span)
	traceID := TraceIDFromContext(ctx)
	assert.NotEmpty(t, traceID)
}

// --- 辅助函数 ---

// resetOnce 重置 sync.Once（仅用于测试）。
func resetOnce() {
	once = sync.Once{}
	shutdownOnceFn = sync.OnceFunc(func() {
		if tp != nil {
			_ = tp.Shutdown(context.Background())
		}
	})
	tp = nil
}

// newGRPCMetadata 创建一个空的 gRPC metadata。
func newGRPCMetadata() metadata.MD {
	return metadata.MD{}
}

// readFile 读取文件内容。
func readFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
