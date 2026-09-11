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
	//
	// 无法用本字段表达"不主动采样，仅跟随上游"：0 会被视为未设置并填充为 1.0。
	// 需要该语义请用 WithSampler(0)，它与 Disabled 不等价：
	//   - Sampler = 0：TracerProvider 正常创建，根 span 不采样，
	//     但上游已采样的链路仍会继续上报
	//   - Disabled = true：根本不创建 TracerProvider，任何链路都不上报
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
	// OtlpGrpcSecure OTLP gRPC 是否使用 TLS，默认 false。
	// 为 true 时不注入 WithInsecure，可连接启用了 TLS 的 collector（如 443 端口）。
	OtlpGrpcSecure bool `json:",optional"`
	// Disabled 是否禁用链路追踪，默认 false。
	// 设为 true 时 StartAgent 不会启动任何导出器。
	Disabled bool `json:",optional"`
}

// Option 覆盖配置项，用于表达 Config 结构体无法表达的显式零值。
//
// 为什么需要：fillDefault 遵循「字段 == 0 视为未设置」的规则，
// 而 Sampler = 0 是一个有别于默认值 1.0 的有效配置
// （只跟随上游采样，自己不主动采样），详见 Config.Sampler 的说明。
//
// 通过 Option 传入的值会在默认值填充之后应用，因此能够生效：
//
//	trace.StartAgent(cfg, trace.WithSampler(0))
type Option func(*Config)

// WithSampler 显式设置采样率，取值范围 0.0~1.0。
// 与 Config.Sampler 的区别：传 0 不会被填充为默认值 1.0，
// 而是表示根 span 不采样、仅跟随上游采样（与 Disabled 不等价）。
// 越界值由 otel 内部处理：>= 1 视为全采样，< 0 视为 0。
func WithSampler(ratio float64) Option {
	return func(c *Config) { c.Sampler = ratio }
}
