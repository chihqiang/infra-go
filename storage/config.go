package storage

// Config 存储服务配置。
// 默认值通过结构体标签 default 定义，遵循 conf 标准。
type Config struct {
	// Driver 存储驱动类型，支持 "oss"、"cos" 和 "kodo"，必填。
	Driver Driver `json:"driver"`

	// OSS 阿里云 OSS 配置，当 Driver 为 "oss" 时使用，默认空。
	OSS *OSSConfig `json:",optional"`

	// COS 腾讯云 COS 配置，当 Driver 为 "cos" 时使用，默认空。
	COS *COSConfig `json:",optional"`

	// KODO 七牛云 KODO 配置，当 Driver 为 "kodo" 时使用，默认空。
	KODO *KODOConfig `json:",optional"`
}

// OSSConfig 阿里云 OSS 存储配置。
type OSSConfig struct {
	// Endpoint OSS 访问域名，例如 "oss-cn-hangzhou.aliyuncs.com"，必填。
	// 完整列表参考：https://help.aliyun.com/zh/oss/user-guide/regions-and-endpoints
	Endpoint string `json:"endpoint"`

	// AccessKeyID 阿里云 AccessKey ID，必填。
	AccessKeyID string `json:"access_key_id"`

	// AccessKeySecret 阿里云 AccessKey Secret，必填。
	AccessKeySecret string `json:"access_key_secret"`

	// Bucket 存储空间名称，必填。
	Bucket string `json:"bucket"`

	// URL 文件访问域名（CDN 或自定义域名），用于拼接完整访问 URL。
	// 为空时默认使用 "https://{bucket}.{endpoint}"，默认空。
	URL string `json:",optional"`
}

// COSConfig 腾讯云 COS 存储配置。
type COSConfig struct {
	// BucketURL 存储桶访问地址，例如 "https://bucket-name.cos.ap-beijing.myqcloud.com"，必填。
	// 完整列表参考：https://console.cloud.tencent.com/cos5/bucket
	BucketURL string `json:"bucket_url"`

	// SecretID 腾讯云 SecretID，必填。
	// 参考：https://cloud.tencent.com/document/product/598/37140
	SecretID string `json:"secret_id"`

	// SecretKey 腾讯云 SecretKey，必填。
	SecretKey string `json:"secret_key"`

	// URL 文件访问域名（CDN 或自定义域名），用于拼接完整访问 URL。
	// 为空时默认使用 BucketURL，默认空。
	URL string `json:",optional"`
}

// KODOConfig 七牛云 KODO 存储配置。
// 默认值通过结构体标签 default 定义，遵循 conf 标准。
type KODOConfig struct {
	// AccessKey 七牛云 AccessKey，必填。
	AccessKey string `json:"access_key"`

	// SecretKey 七牛云 SecretKey，必填。
	SecretKey string `json:"secret_key"`

	// Bucket 存储空间名称，必填。
	Bucket string `json:"bucket"`

	// Region 存储区域，例如 "z0"（华东）、"z1"（华北）、"z2"（华南），默认 "z0"。
	// 参考：https://developer.qiniu.com/kodo/manual/1671/region-endpoint-fq
	Region string `json:",default=z0"`

	// URL 文件访问域名（CDN 或绑定的自定义域名），用于拼接完整访问 URL。
	// 七牛云必须绑定域名才能公开访问，因此调用 URL() 方法时此字段必填，默认空。
	URL string `json:",optional"`
}
