package trace

import (
	"context"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
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
		Name:           "my-service",
		Endpoint:       "localhost:4317",
		Sampler:        0.5,
		Batcher:        BatcherZipkin,
		Disabled:       true,
		OtlpHeaders:    map[string]string{"key": "val"},
		OtlpHttpPath:   "/v1/traces",
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

// TestFillDefault_SamplerZeroViaOption 验证 Sampler=0（根 span 不采样、
// 仅跟随上游采样）可通过 Option 表达，不被默认值 1.0 覆盖。
func TestFillDefault_SamplerZeroViaOption(t *testing.T) {
	c := fillDefault(Config{}, WithSampler(0))
	assert.InDelta(t, 0.0, c.Sampler, 0.001)
}

// TestFillDefault_SamplerZeroWithoutOption 锁定已知局限：直接写 Config.Sampler = 0
// 仍被当作未设置并回落默认 1.0，需要显式 0 时必须用 WithSampler(0)。
func TestFillDefault_SamplerZeroWithoutOption(t *testing.T) {
	c := fillDefault(Config{Sampler: 0})
	assert.InDelta(t, 1.0, c.Sampler, 0.001)
}

// TestStartAgent_SamplerZeroViaOption 验证 WithSampler(0) 真正生效：
// TracerProvider 仍被创建，但根 span 不被采样、不会被导出。
// 这正是它与 Disabled 的区别（后者根本不创建 provider）。
func TestStartAgent_SamplerZeroViaOption(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := tmpDir + "/trace.log"

	resetOnce()
	StartAgent(Config{
		Name:     "test-service",
		Endpoint: logFile,
		Batcher:  BatcherFile,
	}, WithSampler(0))
	defer func() {
		StopAgent()
		resetOnce()
	}()

	tracer := otel.Tracer(TraceName)
	ctx, span := tracer.Start(context.Background(), "root-operation")
	span.End()
	assert.False(t, span.SpanContext().IsSampled(), "WithSampler(0) 下根 span 不应被采样")

	require.NoError(t, span.TracerProvider().(interface {
		ForceFlush(context.Context) error
	}).ForceFlush(ctx))

	data, err := readFile(logFile)
	require.NoError(t, err)
	assert.Empty(t, data, "未采样的 span 不应被导出")
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

// resetOnce 重置 agent 生命周期状态（仅用于测试）。
//
// 现在 StartAgent/StopAgent 通过锁 + currentAgent 管理生命周期，
// 不再依赖 sync.Once，因此测试只需把状态清空即可重复启动。
func resetOnce() {
	StopAgent()
	agentLk.Lock()
	currentAgent = nil
	agentLk.Unlock()
}

// --- 并发安全（回归）---

// TestAddResources_Concurrent 回归测试：并发 AddResources 与读取不得竞争。
//
// 历史缺陷：attrResources 是无锁的包级切片，AddResources 直接 append，
// startAgent 直接读取，-race 可检出数据竞争。
func TestAddResources_Concurrent(t *testing.T) {
	resetResources()
	t.Cleanup(resetResources)

	const workers = 16
	const iterations = 100

	var wg sync.WaitGroup
	// 并发写入
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				AddResources(AttrString("k", "v"))
			}
		}()
	}
	// 并发读取（模拟 startAgent 构造 Resource）。
	// 此处不断言长度：读取与写入并发，长度只保证单调递增，不保证等于最终值。
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				got := resourceAttrs()
				assert.NotNil(t, got)
			}
		}()
	}
	wg.Wait()

	assert.Len(t, resourceAttrs(), workers*iterations)
}

// TestAddResources_SnapshotIsACopy 验证 resourceAttrs 返回副本，
// 调用方修改不会影响内部状态。
func TestAddResources_SnapshotIsACopy(t *testing.T) {
	resetResources()
	defer resetResources()

	AddResources(AttrString("env", "test"))
	got := resourceAttrs()
	require.Len(t, got, 1)

	got[0] = AttrString("mutated", "x")
	assert.Equal(t, "env", string(resourceAttrs()[0].Key))
}

// TestStartAgent_RestartAfterStop 回归测试：StopAgent 之后必须能重新启动。
//
// 历史缺陷：StartAgent 使用 sync.Once，StopAgent 之后 once 仍为已执行状态，
// 再次 StartAgent 无效，而 otel 全局 provider 仍指向已 Shutdown 的实例，
// 导致后续 span 被静默丢弃。
func TestStartAgent_RestartAfterStop(t *testing.T) {
	resetOnce()
	t.Cleanup(resetOnce)

	dir := t.TempDir()

	// 第一次启动
	StartAgent(Config{
		Name:     "first",
		Endpoint: dir + "/first.log",
		Batcher:  BatcherFile,
		Sampler:  1.0,
	})
	require.NotNil(t, currentAgent, "first StartAgent should establish an agent")
	first := currentAgent

	StopAgent()
	assert.Nil(t, currentAgent, "StopAgent should clear the agent state")

	// 再次启动：应建立新的 agent，而不是被忽略
	StartAgent(Config{
		Name:     "second",
		Endpoint: dir + "/second.log",
		Batcher:  BatcherFile,
		Sampler:  1.0,
	})
	require.NotNil(t, currentAgent, "StartAgent after StopAgent must work")
	assert.NotSame(t, first, currentAgent, "a new agent state must be created")
}

// TestStartAgent_AlreadyRunningIsIgnored 验证运行期间重复 StartAgent 被忽略并记录警告。
func TestStartAgent_AlreadyRunningIsIgnored(t *testing.T) {
	resetOnce()
	t.Cleanup(resetOnce)

	dir := t.TempDir()
	StartAgent(Config{Name: "s", Endpoint: dir + "/a.log", Batcher: BatcherFile})
	first := currentAgent
	require.NotNil(t, first)

	// 第二次调用（未 Stop）：应保持原 agent
	StartAgent(Config{Name: "other", Endpoint: dir + "/b.log", Batcher: BatcherFile})
	assert.Same(t, first, currentAgent, "a second StartAgent must not replace the running agent")
}

// TestStopAgent_Idempotent 验证 StopAgent 可重复调用。
func TestStopAgent_Idempotent(t *testing.T) {
	resetOnce()

	dir := t.TempDir()
	StartAgent(Config{Name: "s", Endpoint: dir + "/a.log", Batcher: BatcherFile})

	require.NotPanics(t, func() {
		StopAgent()
		StopAgent()
		StopAgent()
	})
	assert.Nil(t, currentAgent)
}

// TestStopAgent_ClosesFileExporter 回归测试：StopAgent 必须调用注册的 closers
// 释放 file 导出器的文件句柄。
//
// 历史缺陷：closer 存在包级变量 fileCloser 中，StopAgent 从不调用它，
// 文件句柄在进程生命周期内不释放（且重复启动会覆盖变量，先前句柄彻底丢失）。
func TestStopAgent_ClosesFileExporter(t *testing.T) {
	resetOnce()
	t.Cleanup(resetOnce)

	dir := t.TempDir()
	path := dir + "/trace.log"

	StartAgent(Config{Name: "s", Endpoint: path, Batcher: BatcherFile, Sampler: 1.0})
	require.NotNil(t, currentAgent)
	require.Len(t, currentAgent.closers, 1, "file exporter must register a closer")

	// 用哨兵替换 closer，验证 StopAgent 确实调用了它
	called := atomic.Int32{}
	currentAgent.closers[0] = func() error {
		called.Add(1)
		return nil
	}

	StopAgent()

	assert.Equal(t, int32(1), called.Load(),
		"StopAgent must invoke the closers registered by the exporter")
	assert.Nil(t, currentAgent)
}

// TestStopAgent_ClosesRealFileHandle 验证 file 导出器的句柄被真正释放：
// 关闭后再次以同一路径启动仍可正常工作（不会因句柄泄漏而失败）。
func TestStopAgent_ClosesRealFileHandle(t *testing.T) {
	resetOnce()
	t.Cleanup(resetOnce)

	dir := t.TempDir()
	path := dir + "/trace.log"

	// 反复启动/停止，模拟配置重载；若有句柄泄漏，此处能暴露资源累积
	for i := 0; i < 20; i++ {
		StartAgent(Config{Name: "s", Endpoint: path, Batcher: BatcherFile, Sampler: 1.0})
		require.NotNil(t, currentAgent, "iteration %d", i)
		StopAgent()
		require.Nil(t, currentAgent, "iteration %d", i)
	}

	// 文件应存在且可读
	_, err := os.Stat(path)
	require.NoError(t, err)
}

// TestStartAgent_Concurrent 验证并发 StartAgent/StopAgent 不产生竞争或 panic。
func TestStartAgent_Concurrent(t *testing.T) {
	resetOnce()
	t.Cleanup(resetOnce)

	dir := t.TempDir()

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			StartAgent(Config{
				Name:     "concurrent",
				Endpoint: dir + "/c.log",
				Batcher:  BatcherFile,
			})
		}(i)
	}
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			StopAgent()
		}()
	}
	wg.Wait()

	StopAgent()
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
