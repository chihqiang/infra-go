package trace

// TraceName 链路追踪名称。
const TraceName = "infra-go"

// Batcher 导出器类型。
type Batcher string

const (
	// BatcherOTLPGRPC 使用 OTLP gRPC 导出链路数据。
	BatcherOTLPGRPC Batcher = "otlpgrpc"
	// BatcherOTLPHTTP 使用 OTLP HTTP 导出链路数据。
	BatcherOTLPHTTP Batcher = "otlphttp"
	// BatcherZipkin 使用 Zipkin 导出链路数据。
	BatcherZipkin Batcher = "zipkin"
	// BatcherFile 输出链路数据到文件。
	BatcherFile Batcher = "file"
)

// Config 链路追踪配置。
// 默认值通过结构体标签 default 定义，遵循 conf 标准。
type Config struct {
	// Name 服务名称，用于标识链路来源，默认 "infra-go"。
	Name string `json:",default=infra-go"`
	// Endpoint 导出器地址。
	// Batcher 为 file 时为文件路径。
	// 其他类型时为导出器服务地址，例如 "localhost:4317"。
	Endpoint string `json:",optional"`
	// Sampler 采样率，取值范围 0.0~1.0，默认 1.0（全采样）。
	Sampler float64 `json:",default=1.0"`
	// Batcher 导出器类型，默认 "otlpgrpc"。
	// 可选值：otlpgrpc、otlphttp、zipkin、file。
	Batcher Batcher `json:",default=otlpgrpc"`
	// OtlpHeaders OTLP gRPC/HTTP 传输的自定义请求头。
	OtlpHeaders map[string]string `json:",optional"`
	// OtlpHttpPath OTLP HTTP 传输的路径，例如 "/v1/traces"。
	OtlpHttpPath string `json:",optional"`
	// OtlpHttpSecure OTLP HTTP 是否使用 HTTPS，默认 false。
	OtlpHttpSecure bool `json:",optional"`
	// Disabled 是否禁用链路追踪，默认 false。
	// 设为 true 时 StartAgent 不会启动任何导出器。
	Disabled bool `json:",optional"`
}
