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

// agentState holds the resources of one agent lifecycle.
type agentState struct {
	tp *sdktrace.TracerProvider
	// closers must be released in StopAgent (e.g. the file handle of the file exporter).
	// They are owned by the agent itself instead of living in package-level variables:
	// the latter overwrite each other with multiple instances or repeated starts, so
	// previously opened files could never be closed (handle leak).
	closers []func() error
}

var (
	// agentLk guards currentAgent so StartAgent/StopAgent can be called concurrently.
	agentLk      sync.Mutex
	currentAgent *agentState
)

// StartAgent starts the tracing agent.
//
// When called repeatedly the behaviour is:
//   - an agent is already running (StopAgent was not called) → the new config is
//     ignored and a warning is logged.
//     The config is snapshotted when the agent starts and cannot be hot-reloaded;
//     to change it, call StopAgent first and then StartAgent.
//   - StopAgent has already been called → starting again is allowed (with the new
//     config).
//
// opts express explicit zero values that the Config struct cannot represent
// (such as Sampler = 0); see Option.
//
// The old implementation used sync.Once for "initialise only once", with the side
// effect that nothing could be restarted after StopAgent, while otel still pointed
// at a shut-down TracerProvider, so later spans were silently dropped.
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

// StopAgent shuts the tracing agent down, flushes spans that were not exported yet
// and releases the handles held by the exporters.
// It is normally called before the program exits; it is idempotent and the agent can
// be started again afterwards.
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
	// Close resources such as files to avoid leaking handles
	for _, closeFn := range st.closers {
		if closeFn != nil {
			_ = closeFn()
		}
	}
}

// startAgent is the internal implementation that starts the agent and returns the
// state of this lifecycle.
func startAgent(c Config) (*agentState, error) {
	// Add the service name resource attribute
	AddResources(semconv.ServiceNameKey.String(c.Name))

	opts := []sdktrace.TracerProviderOption{
		// Sampler ratio based on the parent span
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(c.Sampler))),
		// Record application information into the Resource (snapshots the current
		// attributes; ones added later take no effect)
		sdktrace.WithResource(resource.NewSchemaless(resourceAttrs()...)),
	}

	st := &agentState{}

	// Configure the exporter
	if len(c.Endpoint) > 0 {
		exp, closers, err := createExporterWithClosers(c)
		if err != nil {
			return nil, fmt.Errorf("failed to create trace exporter: %w", err)
		}
		st.closers = closers
		// Production environments use batch export
		opts = append(opts, sdktrace.WithBatcher(exp))
	}

	st.tp = sdktrace.NewTracerProvider(opts...)

	// Set the global TracerProvider
	otel.SetTracerProvider(st.tp)

	// Set the error handler, forwarding otel internal errors to the logger
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		logger.Error(fmt.Sprintf("[otel] error: %v", err))
	}))

	return st, nil
}

// createExporterWithClosers creates the exporter and returns the close functions that
// must be released with the agent lifecycle.
//
// The close functions are held by the caller (startAgent → agentState.closers) and
// released in StopAgent; the single caller must handle the return value, otherwise
// the file handle of the file exporter leaks.
func createExporterWithClosers(c Config) (sdktrace.SpanExporter, []func() error, error) {
	switch c.Batcher {
	case BatcherZipkin:
		exp, err := zipkin.New(c.Endpoint)
		return exp, nil, err

	case BatcherOTLPGRPC:
		// Use non-blocking mode so an unreachable exporter does not slow down startup
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
			// Release the already opened file immediately when creation fails, to avoid
			// leaking the handle
			_ = closeFn()
			return nil, nil, err
		}
		return exp, []func() error{closeFn}, nil

	default:
		return nil, nil, fmt.Errorf("unsupported batcher type: %s", c.Batcher)
	}
}
