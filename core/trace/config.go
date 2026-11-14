package trace

// TraceName is the tracing name.
const TraceName = "infra-go"

// Batcher is the exporter type.
type Batcher string

const (
	// BatcherOTLPGRPC exports trace data over OTLP gRPC.
	BatcherOTLPGRPC Batcher = "otlpgrpc"
	// BatcherOTLPHTTP exports trace data over OTLP HTTP.
	BatcherOTLPHTTP Batcher = "otlphttp"
	// BatcherZipkin exports trace data to Zipkin.
	BatcherZipkin Batcher = "zipkin"
	// BatcherFile writes trace data to a file.
	BatcherFile Batcher = "file"
)

// Config is the tracing configuration.
// Defaults are defined by the default struct tag, following the conf standard.
type Config struct {
	// Name is the service name used to identify the trace source, default "infra-go".
	Name string `json:",default=infra-go"`
	// Endpoint is the exporter address.
	// When Batcher is file it is a file path.
	// For the other types it is the exporter service address, e.g. "localhost:4317".
	Endpoint string `json:",optional"`
	// Sampler is the sampling ratio in the range 0.0~1.0, default 1.0 (sample everything).
	//
	// "Do not sample itself, only follow upstream" cannot be expressed with this field:
	// 0 is treated as unset and filled in with 1.0.
	// Use WithSampler(0) for that semantic; it is not equivalent to Disabled:
	//   - Sampler = 0: the TracerProvider is created normally and the root span is not
	//     sampled, but traces already sampled upstream keep being reported
	//   - Disabled = true: no TracerProvider is created at all and no trace is reported
	Sampler float64 `json:",default=1.0"`
	// Batcher is the exporter type, default "otlpgrpc".
	// Possible values: otlpgrpc, otlphttp, zipkin, file.
	Batcher Batcher `json:",default=otlpgrpc"`
	// OtlpHeaders holds custom request headers for OTLP gRPC/HTTP transport.
	OtlpHeaders map[string]string `json:",optional"`
	// OtlpHttpPath is the OTLP HTTP transport path, e.g. "/v1/traces".
	OtlpHttpPath string `json:",optional"`
	// OtlpHttpSecure reports whether OTLP HTTP uses HTTPS, default false.
	OtlpHttpSecure bool `json:",optional"`
	// OtlpGrpcSecure reports whether OTLP gRPC uses TLS, default false.
	// When true, WithInsecure is not injected, so a collector with TLS enabled
	// (e.g. port 443) can be reached.
	OtlpGrpcSecure bool `json:",optional"`
	// Disabled reports whether tracing is disabled, default false.
	// When true, StartAgent starts no exporter at all.
	Disabled bool `json:",optional"`
}

// Option overrides configuration entries; it expresses explicit zero values that the
// Config struct cannot represent.
//
// Why it is needed: fillDefault follows the rule "a zero field counts as unset", while
// Sampler = 0 is a valid configuration that differs from the default 1.0 (follow the
// upstream sampling only, do not sample on its own); see the description on
// Config.Sampler.
//
// Values passed through an Option are applied after the defaults are filled in, so they
// do take effect:
//
//	trace.StartAgent(cfg, trace.WithSampler(0))
type Option func(*Config)

// WithSampler sets the sampling ratio explicitly, in the range 0.0~1.0.
// Difference from Config.Sampler: passing 0 is not filled in with the default 1.0, but
// means the root span is not sampled and only upstream sampling is followed (not
// equivalent to Disabled).
// Out-of-range values are handled inside otel: >= 1 counts as sampling everything,
// < 0 counts as 0.
func WithSampler(ratio float64) Option {
	return func(c *Config) { c.Sampler = ratio }
}
