package trace

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/grpc/metadata"
)

// resetResources resets the global resource attributes so test cases do not affect
// each other (tests only).
func resetResources() {
	attrResourcesLk.Lock()
	attrResources = make([]attribute.KeyValue, 0)
	attrResourcesLk.Unlock()
}

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
	// Nothing should be initialised when Disabled is true
	assert.NotPanics(t, func() {
		StartAgent(Config{Disabled: true})
	})
}

// TestFillDefault_SamplerZeroViaOption verifies that Sampler=0 (the root span is not
// sampled, only upstream sampling is followed) can be expressed through an Option and
// is not overwritten by the default 1.0.
func TestFillDefault_SamplerZeroViaOption(t *testing.T) {
	c := fillDefault(Config{}, WithSampler(0))
	assert.InDelta(t, 0.0, c.Sampler, 0.001)
}

// TestFillDefault_SamplerZeroWithoutOption pins down a known limitation: writing
// Config.Sampler = 0 directly is still treated as unset and falls back to the default
// 1.0, so an explicit 0 requires WithSampler(0).
func TestFillDefault_SamplerZeroWithoutOption(t *testing.T) {
	c := fillDefault(Config{Sampler: 0})
	assert.InDelta(t, 1.0, c.Sampler, 0.001)
}

// TestStartAgent_SamplerZeroViaOption verifies that WithSampler(0) really takes effect:
// the TracerProvider is still created, but the root span is not sampled and is not
// exported.
// This is exactly how it differs from Disabled (which creates no provider at all).
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
	assert.False(t, span.SpanContext().IsSampled(),
		"the root span must not be sampled with WithSampler(0)")

	require.NoError(t, span.TracerProvider().(interface {
		ForceFlush(context.Context) error
	}).ForceFlush(ctx))

	data, err := readFile(logFile)
	require.NoError(t, err)
	assert.Empty(t, data, "an unsampled span must not be exported")
}

func TestStartAgent_FileExporter(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := tmpDir + "/trace.log"

	// Reset the state so the test can be repeated
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

	// Create a span to verify the exporter works
	tracer := otel.Tracer(TraceName)
	ctx, span := tracer.Start(context.Background(), "test-operation")
	span.End()

	// Make sure everything is flushed
	require.NoError(t, span.TracerProvider().(interface {
		ForceFlush(context.Context) error
	}).ForceFlush(ctx))

	// Verify the file has content
	data, err := readFile(logFile)
	require.NoError(t, err)
	assert.NotEmpty(t, data)
}

func TestStartAgent_NoEndpoint(t *testing.T) {
	// Without an Endpoint no exporter is created, but the tracer provider is still
	// initialised
	resetOnce()

	StartAgent(Config{
		Name:    "test-service",
		Sampler: 1.0,
	})
	defer func() {
		StopAgent()
		resetOnce()
	}()

	// Creating a span must not panic
	tracer := otel.Tracer(TraceName)
	_, span := tracer.Start(context.Background(), "test-no-export")
	span.End()
}

func TestStopAgent_MultipleCalls(t *testing.T) {
	// Calling StopAgent multiple times must not panic
	assert.NotPanics(t, func() {
		StopAgent()
		StopAgent()
	})
}

func TestCreateExporter_UnsupportedBatcher(t *testing.T) {
	_, closers, err := createExporterWithClosers(Config{
		Batcher:  "unsupported",
		Endpoint: "localhost:4317",
	})
	require.Error(t, err)
	assert.Nil(t, closers)
	assert.Contains(t, err.Error(), "unsupported batcher type")
}

func TestCreateExporter_FileError(t *testing.T) {
	_, closers, err := createExporterWithClosers(Config{
		Batcher:  BatcherFile,
		Endpoint: "/nonexistent_dir/deep/path/trace.log",
	})
	require.Error(t, err)
	assert.Nil(t, closers)
	assert.Contains(t, err.Error(), "file exporter endpoint error")
}

// The file exporter opens a file handle; the close function must be handed back to the
// caller, otherwise StopAgent cannot release it.
func TestCreateExporterWithClosers_FileReturnsCloser(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trace.log")

	_, closers, err := createExporterWithClosers(Config{
		Batcher:  BatcherFile,
		Endpoint: path,
	})
	require.NoError(t, err)
	require.Len(t, closers, 1,
		"the file exporter must return a close function, otherwise the file handle leaks")
	assert.NoError(t, closers[0]())
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
	// A context without a span should yield an empty string
	ctx := context.Background()
	assert.Equal(t, "", TraceIDFromContext(ctx))
}

func TestSpanIDFromContext_Empty(t *testing.T) {
	ctx := context.Background()
	assert.Equal(t, "", SpanIDFromContext(ctx))
}

func TestTracerFromContext_Global(t *testing.T) {
	// A context without a span should yield the global tracer
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

	// After starting a span the context should hold a trace id
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

	// Create a span
	ctx, span := StartSpan(context.Background(), "parent-op")
	defer span.End()

	// Inject into gRPC metadata
	md := newGRPCMetadata()
	Inject(ctx, &md)

	// Extract from the metadata
	extractedCtx, sc := Extract(context.Background(), &md)
	assert.True(t, sc.IsValid())

	// The extracted trace id should match the original one
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

	// Create a span
	ctx, span := StartSpan(context.Background(), "parent-http-op")
	defer span.End()

	// Inject into the HTTP header
	header := http.Header{}
	InjectHeader(ctx, header)

	// Extract from the header
	extractedCtx, sc := ExtractHeader(context.Background(), header)
	assert.True(t, sc.IsValid())

	// The extracted trace id should match the original one
	extractedTraceID := TraceIDFromContext(extractedCtx)
	originalTraceID := TraceIDFromContext(ctx)
	assert.Equal(t, originalTraceID, extractedTraceID)
}

func TestInjectExtract_NoSpan(t *testing.T) {
	// Injecting a context without a span and extracting again must yield no valid span
	// context
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

	// The span should be created normally
	assert.NotNil(t, span)
	traceID := TraceIDFromContext(ctx)
	assert.NotEmpty(t, traceID)
}

// --- Helper functions ---

// resetOnce resets the agent lifecycle state (tests only).
//
// StartAgent/StopAgent now manage the lifecycle with a lock plus currentAgent and no
// longer rely on sync.Once, so tests only have to clear the state to start again.
func resetOnce() {
	StopAgent()
	agentLk.Lock()
	currentAgent = nil
	agentLk.Unlock()
}

// --- Concurrency safety (regressions) ---

// TestAddResources_Concurrent is a regression test: concurrent AddResources and reads
// must not race.
//
// Historical defect: attrResources was an unlocked package-level slice, AddResources
// appended to it directly and startAgent read it directly, so -race detected a data
// race.
func TestAddResources_Concurrent(t *testing.T) {
	resetResources()
	t.Cleanup(resetResources)

	const workers = 16
	const iterations = 100

	var wg sync.WaitGroup
	// Concurrent writers
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				AddResources(AttrString("k", "v"))
			}
		}()
	}
	// Concurrent readers (simulating startAgent building the Resource).
	// The length is not asserted here: reads run concurrently with writes, so the length
	// is only guaranteed to grow monotonically, not to equal the final value.
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

// TestAddResources_SnapshotIsACopy verifies that resourceAttrs returns a copy, so a
// caller mutating it does not affect the internal state.
func TestAddResources_SnapshotIsACopy(t *testing.T) {
	resetResources()
	defer resetResources()

	AddResources(AttrString("env", "test"))
	got := resourceAttrs()
	require.Len(t, got, 1)

	got[0] = AttrString("mutated", "x")
	assert.Equal(t, "env", string(resourceAttrs()[0].Key))
}

// TestStartAgent_RestartAfterStop is a regression test: the agent must be startable
// again after StopAgent.
//
// Historical defect: StartAgent used sync.Once, so once StopAgent had run the once was
// still marked as done, another StartAgent did nothing, and the otel global provider
// still pointed at a shut-down instance, so later spans were silently dropped.
func TestStartAgent_RestartAfterStop(t *testing.T) {
	resetOnce()
	t.Cleanup(resetOnce)

	dir := t.TempDir()

	// First start
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

	// Start again: a new agent should be established instead of the call being ignored
	StartAgent(Config{
		Name:     "second",
		Endpoint: dir + "/second.log",
		Batcher:  BatcherFile,
		Sampler:  1.0,
	})
	require.NotNil(t, currentAgent, "StartAgent after StopAgent must work")
	assert.NotSame(t, first, currentAgent, "a new agent state must be created")
}

// TestStartAgent_AlreadyRunningIsIgnored verifies that a repeated StartAgent while one
// is running is ignored and logs a warning.
func TestStartAgent_AlreadyRunningIsIgnored(t *testing.T) {
	resetOnce()
	t.Cleanup(resetOnce)

	dir := t.TempDir()
	StartAgent(Config{Name: "s", Endpoint: dir + "/a.log", Batcher: BatcherFile})
	first := currentAgent
	require.NotNil(t, first)

	// Second call (without Stop): the original agent must be kept
	StartAgent(Config{Name: "other", Endpoint: dir + "/b.log", Batcher: BatcherFile})
	assert.Same(t, first, currentAgent, "a second StartAgent must not replace the running agent")
}

// TestStopAgent_Idempotent verifies that StopAgent can be called repeatedly.
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

// TestStopAgent_ClosesFileExporter is a regression test: StopAgent must invoke the
// registered closers to release the file handle of the file exporter.
//
// Historical defect: the closer lived in the package-level variable fileCloser, which
// StopAgent never called, so the file handle was never released for the lifetime of the
// process (and repeated starts overwrote the variable, losing the earlier handle for
// good).
func TestStopAgent_ClosesFileExporter(t *testing.T) {
	resetOnce()
	t.Cleanup(resetOnce)

	dir := t.TempDir()
	path := dir + "/trace.log"

	StartAgent(Config{Name: "s", Endpoint: path, Batcher: BatcherFile, Sampler: 1.0})
	require.NotNil(t, currentAgent)
	require.Len(t, currentAgent.closers, 1, "file exporter must register a closer")

	// Replace the closer with a sentinel to verify StopAgent really calls it
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

// TestStopAgent_ClosesRealFileHandle verifies that the file exporter handle is really
// released: starting again with the same path after closing still works (it does not
// fail because of a leaked handle).
func TestStopAgent_ClosesRealFileHandle(t *testing.T) {
	resetOnce()
	t.Cleanup(resetOnce)

	dir := t.TempDir()
	path := dir + "/trace.log"

	// Start/stop repeatedly to simulate a config reload; a handle leak would show up as
	// accumulating resources here
	for i := 0; i < 20; i++ {
		StartAgent(Config{Name: "s", Endpoint: path, Batcher: BatcherFile, Sampler: 1.0})
		require.NotNil(t, currentAgent, "iteration %d", i)
		StopAgent()
		require.Nil(t, currentAgent, "iteration %d", i)
	}

	// The file should exist and be readable
	_, err := os.Stat(path)
	require.NoError(t, err)
}

// TestStartAgent_Concurrent verifies that concurrent StartAgent/StopAgent calls do not
// race or panic.
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

// newGRPCMetadata creates an empty gRPC metadata.
func newGRPCMetadata() metadata.MD {
	return metadata.MD{}
}

// readFile reads the file contents.
func readFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
