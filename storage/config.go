package storage

// Config is the storage service configuration.
// Default values are defined via the `default` struct tag, following the conf standard.
type Config struct {
	// Driver is the storage driver type; "local", "oss", "cos" and "kodo" are
	// supported. Required.
	Driver Driver `json:"driver"`

	// Local is the local filesystem configuration, used when Driver is "local".
	// Empty by default.
	Local *LocalConfig `json:",optional"`

	// OSS is the Alibaba Cloud OSS configuration, used when Driver is "oss".
	// Empty by default.
	OSS *OSSConfig `json:",optional"`

	// COS is the Tencent Cloud COS configuration, used when Driver is "cos".
	// Empty by default.
	COS *COSConfig `json:",optional"`

	// KODO is the Qiniu Cloud KODO configuration, used when Driver is "kodo".
	// Empty by default.
	KODO *KODOConfig `json:",optional"`
}

// LocalConfig is the local filesystem storage configuration.
type LocalConfig struct {
	// RootDir is the local storage root directory. Required.
	// Files are written under this directory; sub-paths in path map to relative
	// directories below the root.
	RootDir string `json:"root_dir"`

	// URL is the access URL prefix (optional), for example the address of a
	// static file server such as "http://localhost:8080/static".
	// When empty, URL() returns a local absolute path with the file:// scheme.
	URL string `json:",optional"`
}

// OSSConfig is the Alibaba Cloud OSS storage configuration.
type OSSConfig struct {
	// Endpoint is the OSS access domain, for example
	// "oss-cn-hangzhou.aliyuncs.com". Required.
	// Full list: https://help.aliyun.com/zh/oss/user-guide/regions-and-endpoints
	Endpoint string `json:"endpoint"`

	// AccessKeyID is the Alibaba Cloud AccessKey ID. Required.
	AccessKeyID string `json:"access_key_id"`

	// AccessKeySecret is the Alibaba Cloud AccessKey Secret. Required.
	AccessKeySecret string `json:"access_key_secret"`

	// Bucket is the bucket name. Required.
	Bucket string `json:"bucket"`

	// URL is the file access domain (CDN or custom domain) used to build the full
	// access URL. When empty it defaults to "https://{bucket}.{endpoint}".
	// Empty by default.
	URL string `json:",optional"`
}

// COSConfig is the Tencent Cloud COS storage configuration.
type COSConfig struct {
	// BucketURL is the bucket access address, for example
	// "https://bucket-name.cos.ap-beijing.myqcloud.com". Required.
	// Full list: https://console.cloud.tencent.com/cos5/bucket
	BucketURL string `json:"bucket_url"`

	// SecretID is the Tencent Cloud SecretID. Required.
	// Reference: https://cloud.tencent.com/document/product/598/37140
	SecretID string `json:"secret_id"`

	// SecretKey is the Tencent Cloud SecretKey. Required.
	SecretKey string `json:"secret_key"`

	// URL is the file access domain (CDN or custom domain) used to build the full
	// access URL. When empty it defaults to BucketURL. Empty by default.
	URL string `json:",optional"`
}

// KODOConfig is the Qiniu Cloud KODO storage configuration.
// Default values are defined via the `default` struct tag, following the conf standard.
type KODOConfig struct {
	// AccessKey is the Qiniu Cloud AccessKey. Required.
	AccessKey string `json:"access_key"`

	// SecretKey is the Qiniu Cloud SecretKey. Required.
	SecretKey string `json:"secret_key"`

	// Bucket is the bucket name. Required.
	Bucket string `json:"bucket"`

	// Region is the storage region, for example "z0" (East China), "z1"
	// (North China) or "z2" (South China). Defaults to "z0".
	// Reference: https://developer.qiniu.com/kodo/manual/1671/region-endpoint-fq
	Region string `json:",default=z0"`

	// URL is the file access domain (CDN or a bound custom domain) used to build
	// the full access URL. Qiniu Cloud requires a bound domain for public access,
	// so this field is required when calling the URL() method. Empty by default.
	URL string `json:",optional"`
}
