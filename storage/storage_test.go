package storage

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Storage 接口 ---

func TestStorageInterface_CompileTimeCheck(t *testing.T) {
	// 编译期验证所有实现都满足 Storage 接口
	var _ Storage = (*ossStorage)(nil)
	var _ Storage = (*cosStorage)(nil)
	var _ Storage = (*kodoStorage)(nil)
}

func TestDriverConstants(t *testing.T) {
	assert.Equal(t, Driver("oss"), DriverOSS)
	assert.Equal(t, Driver("cos"), DriverCOS)
	assert.Equal(t, Driver("kodo"), DriverKODO)
}

// --- buildURL ---

func TestBuildURL(t *testing.T) {
	tests := []struct {
		name    string
		base    string
		path    string
		want    string
		wantErr bool
	}{
		{"normal", "https://cdn.example.com", "path/to/file.txt", "https://cdn.example.com/path/to/file.txt", false},
		{"trailing slash in base", "https://cdn.example.com/", "path/to/file.txt", "https://cdn.example.com/path/to/file.txt", false},
		{"leading slash in path", "https://cdn.example.com", "/path/to/file.txt", "https://cdn.example.com/path/to/file.txt", false},
		{"both slashes", "https://cdn.example.com/", "/path/to/file.txt", "https://cdn.example.com/path/to/file.txt", false},
		{"empty path", "https://cdn.example.com", "", "https://cdn.example.com/", false},
		{"empty base", "", "path/to/file.txt", "", true},
		{"spaces in path", "https://cdn.example.com", "path with spaces/file.txt", "https://cdn.example.com/path%20with%20spaces/file.txt", false},
		{"chinese path", "https://cdn.example.com", "中文路径/file.txt", "https://cdn.example.com/%E4%B8%AD%E6%96%87%E8%B7%AF%E5%BE%84/file.txt", false},
		{"special chars", "https://cdn.example.com", "path/file name.txt", "https://cdn.example.com/path/file%20name.txt", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildURL(tt.base, tt.path)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "base URL is empty")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// --- OSS ---

func TestNewOSS_NilConfig(t *testing.T) {
	_, err := NewOSS(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "OSS config is nil")
}

func TestNewOSS_MissingEndpoint(t *testing.T) {
	_, err := NewOSS(&OSSConfig{
		AccessKeyID:     "id",
		AccessKeySecret: "secret",
		Bucket:          "bucket",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "OSS endpoint is required")
}

func TestNewOSS_MissingAccessKeyID(t *testing.T) {
	_, err := NewOSS(&OSSConfig{
		Endpoint:        "oss-cn-hangzhou.aliyuncs.com",
		AccessKeySecret: "secret",
		Bucket:          "bucket",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "OSS access key ID is required")
}

func TestNewOSS_MissingAccessKeySecret(t *testing.T) {
	_, err := NewOSS(&OSSConfig{
		Endpoint:    "oss-cn-hangzhou.aliyuncs.com",
		AccessKeyID: "id",
		Bucket:      "bucket",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "OSS access key secret is required")
}

func TestNewOSS_MissingBucket(t *testing.T) {
	_, err := NewOSS(&OSSConfig{
		Endpoint:        "oss-cn-hangzhou.aliyuncs.com",
		AccessKeyID:     "id",
		AccessKeySecret: "secret",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "OSS bucket is required")
}

func TestNewOSS_Success(t *testing.T) {
	// oss.New 不会验证凭证，仅创建客户端结构，因此可以用假凭证测试
	s, err := NewOSS(&OSSConfig{
		Endpoint:        "oss-cn-hangzhou.aliyuncs.com",
		AccessKeyID:     "test-access-key-id",
		AccessKeySecret: "test-access-key-secret",
		Bucket:          "test-bucket",
	})
	require.NoError(t, err)
	assert.NotNil(t, s)
}

func TestNewOSS_URL_DefaultFromBucketAndEndpoint(t *testing.T) {
	s, err := NewOSS(&OSSConfig{
		Endpoint:        "oss-cn-hangzhou.aliyuncs.com",
		AccessKeyID:     "test-access-key-id",
		AccessKeySecret: "test-access-key-secret",
		Bucket:          "test-bucket",
	})
	require.NoError(t, err)

	u, err := s.URL("path/to/file.txt")
	require.NoError(t, err)
	assert.Equal(t, "https://test-bucket.oss-cn-hangzhou.aliyuncs.com/path/to/file.txt", u)
}

func TestNewOSS_URL_CustomDomain(t *testing.T) {
	s, err := NewOSS(&OSSConfig{
		Endpoint:        "oss-cn-hangzhou.aliyuncs.com",
		AccessKeyID:     "test-access-key-id",
		AccessKeySecret: "test-access-key-secret",
		Bucket:          "test-bucket",
		URL:             "https://cdn.example.com",
	})
	require.NoError(t, err)

	u, err := s.URL("path/to/file.txt")
	require.NoError(t, err)
	assert.Equal(t, "https://cdn.example.com/path/to/file.txt", u)
}

func TestNewOSS_URL_PathWithLeadingSlash(t *testing.T) {
	s, err := NewOSS(&OSSConfig{
		Endpoint:        "oss-cn-hangzhou.aliyuncs.com",
		AccessKeyID:     "test-access-key-id",
		AccessKeySecret: "test-access-key-secret",
		Bucket:          "test-bucket",
		URL:             "https://cdn.example.com/",
	})
	require.NoError(t, err)

	u, err := s.URL("/path/to/file.txt")
	require.NoError(t, err)
	assert.Equal(t, "https://cdn.example.com/path/to/file.txt", u)
}

func TestNewOSS_URL_EndpointWithProtocol(t *testing.T) {
	s, err := NewOSS(&OSSConfig{
		Endpoint:        "https://oss-cn-hangzhou.aliyuncs.com",
		AccessKeyID:     "test-access-key-id",
		AccessKeySecret: "test-access-key-secret",
		Bucket:          "test-bucket",
	})
	require.NoError(t, err)

	u, err := s.URL("path/to/file.txt")
	require.NoError(t, err)
	assert.Equal(t, "https://test-bucket.oss-cn-hangzhou.aliyuncs.com/path/to/file.txt", u)
}

func TestNewOSS_URL_EndpointWithHTTP(t *testing.T) {
	s, err := NewOSS(&OSSConfig{
		Endpoint:        "http://oss-cn-hangzhou.aliyuncs.com",
		AccessKeyID:     "test-access-key-id",
		AccessKeySecret: "test-access-key-secret",
		Bucket:          "test-bucket",
	})
	require.NoError(t, err)

	u, err := s.URL("path/to/file.txt")
	require.NoError(t, err)
	assert.Equal(t, "http://test-bucket.oss-cn-hangzhou.aliyuncs.com/path/to/file.txt", u)
}

// --- COS ---

func TestNewCOS_NilConfig(t *testing.T) {
	_, err := NewCOS(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "COS config is nil")
}

func TestNewCOS_MissingBucketURL(t *testing.T) {
	_, err := NewCOS(&COSConfig{
		SecretID:  "id",
		SecretKey: "key",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "COS bucket URL is required")
}

func TestNewCOS_MissingSecretID(t *testing.T) {
	_, err := NewCOS(&COSConfig{
		BucketURL: "https://bucket.cos.ap-beijing.myqcloud.com",
		SecretKey: "key",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "COS secret ID is required")
}

func TestNewCOS_MissingSecretKey(t *testing.T) {
	_, err := NewCOS(&COSConfig{
		BucketURL: "https://bucket.cos.ap-beijing.myqcloud.com",
		SecretID:  "id",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "COS secret key is required")
}

func TestNewCOS_InvalidBucketURL(t *testing.T) {
	_, err := NewCOS(&COSConfig{
		BucketURL: "://invalid-url",
		SecretID:  "id",
		SecretKey: "key",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid COS bucket URL")
}

func TestNewCOS_Success(t *testing.T) {
	// cos.NewClient 不会验证凭证，仅创建客户端结构
	s, err := NewCOS(&COSConfig{
		BucketURL: "https://test-bucket.cos.ap-beijing.myqcloud.com",
		SecretID:  "test-secret-id",
		SecretKey: "test-secret-key",
	})
	require.NoError(t, err)
	assert.NotNil(t, s)
}

func TestNewCOS_URL_DefaultFromBucketURL(t *testing.T) {
	s, err := NewCOS(&COSConfig{
		BucketURL: "https://test-bucket.cos.ap-beijing.myqcloud.com",
		SecretID:  "test-secret-id",
		SecretKey: "test-secret-key",
	})
	require.NoError(t, err)

	u, err := s.URL("path/to/file.txt")
	require.NoError(t, err)
	assert.Equal(t, "https://test-bucket.cos.ap-beijing.myqcloud.com/path/to/file.txt", u)
}

func TestNewCOS_URL_CustomDomain(t *testing.T) {
	s, err := NewCOS(&COSConfig{
		BucketURL: "https://test-bucket.cos.ap-beijing.myqcloud.com",
		SecretID:  "test-secret-id",
		SecretKey: "test-secret-key",
		URL:       "https://cdn.example.com",
	})
	require.NoError(t, err)

	u, err := s.URL("path/to/file.txt")
	require.NoError(t, err)
	assert.Equal(t, "https://cdn.example.com/path/to/file.txt", u)
}

// --- KODO ---

func TestNewKODO_NilConfig(t *testing.T) {
	_, err := NewKODO(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "KODO config is nil")
}

func TestNewKODO_MissingAccessKey(t *testing.T) {
	_, err := NewKODO(&KODOConfig{
		SecretKey: "key",
		Bucket:    "bucket",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "KODO access key is required")
}

func TestNewKODO_MissingSecretKey(t *testing.T) {
	_, err := NewKODO(&KODOConfig{
		AccessKey: "key",
		Bucket:    "bucket",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "KODO secret key is required")
}

func TestNewKODO_MissingBucket(t *testing.T) {
	_, err := NewKODO(&KODOConfig{
		AccessKey: "key",
		SecretKey: "key",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "KODO bucket is required")
}

func TestNewKODO_UnsupportedRegion(t *testing.T) {
	_, err := NewKODO(&KODOConfig{
		AccessKey: "test-access-key",
		SecretKey: "test-secret-key",
		Bucket:    "test-bucket",
		Region:    "invalid-region",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported KODO region")
}

func TestNewKODO_Success(t *testing.T) {
	s, err := NewKODO(&KODOConfig{
		AccessKey: "test-access-key",
		SecretKey: "test-secret-key",
		Bucket:    "test-bucket",
	})
	require.NoError(t, err)
	assert.NotNil(t, s)
}

func TestNewKODO_URL_EmptyReturnsError(t *testing.T) {
	s, err := NewKODO(&KODOConfig{
		AccessKey: "test-access-key",
		SecretKey: "test-secret-key",
		Bucket:    "test-bucket",
	})
	require.NoError(t, err)

	_, err = s.URL("path/to/file.txt")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "KODO URL is empty")
}

func TestNewKODO_URL_CustomDomain(t *testing.T) {
	s, err := NewKODO(&KODOConfig{
		AccessKey: "test-access-key",
		SecretKey: "test-secret-key",
		Bucket:    "test-bucket",
		URL:       "https://cdn.example.com",
	})
	require.NoError(t, err)

	u, err := s.URL("path/to/file.txt")
	require.NoError(t, err)
	assert.Equal(t, "https://cdn.example.com/path/to/file.txt", u)
}

func TestNewKODO_DefaultRegion(t *testing.T) {
	// 不指定 Region 时应默认使用 "z0"（华东）
	s, err := NewKODO(&KODOConfig{
		AccessKey: "test-access-key",
		SecretKey: "test-secret-key",
		Bucket:    "test-bucket",
	})
	require.NoError(t, err)
	assert.NotNil(t, s)

	ks, ok := s.(*kodoStorage)
	require.True(t, ok)
	assert.Equal(t, kodoRegions["z0"], ks.storageConfig.Region)
}

func TestNewKODO_AllRegions(t *testing.T) {
	regions := []string{"z0", "z1", "z2", "na0", "as0"}
	for _, region := range regions {
		t.Run(region, func(t *testing.T) {
			s, err := NewKODO(&KODOConfig{
				AccessKey: "test-access-key",
				SecretKey: "test-secret-key",
				Bucket:    "test-bucket",
				Region:    region,
			})
			require.NoError(t, err)
			assert.NotNil(t, s)
		})
	}
}

func TestKodoRegions_AllMapped(t *testing.T) {
	expected := []string{"z0", "z1", "z2", "na0", "as0"}
	for _, r := range expected {
		zone, ok := kodoRegions[r]
		assert.True(t, ok, "region %q should exist in kodoRegions", r)
		assert.NotNil(t, zone, "region %q should map to a non-nil zone", r)
	}
}

// --- 工厂方法 ---

func TestNew_OSS(t *testing.T) {
	s, err := New(Config{
		Driver: DriverOSS,
		OSS: &OSSConfig{
			Endpoint:        "oss-cn-hangzhou.aliyuncs.com",
			AccessKeyID:     "test-access-key-id",
			AccessKeySecret: "test-access-key-secret",
			Bucket:          "test-bucket",
		},
	})
	require.NoError(t, err)
	assert.NotNil(t, s)

	// 验证实际类型
	_, ok := s.(*ossStorage)
	assert.True(t, ok)
}

func TestNew_COS(t *testing.T) {
	s, err := New(Config{
		Driver: DriverCOS,
		COS: &COSConfig{
			BucketURL: "https://test-bucket.cos.ap-beijing.myqcloud.com",
			SecretID:  "test-secret-id",
			SecretKey: "test-secret-key",
		},
	})
	require.NoError(t, err)
	assert.NotNil(t, s)

	_, ok := s.(*cosStorage)
	assert.True(t, ok)
}

func TestNew_KODO(t *testing.T) {
	s, err := New(Config{
		Driver: DriverKODO,
		KODO: &KODOConfig{
			AccessKey: "test-access-key",
			SecretKey: "test-secret-key",
			Bucket:    "test-bucket",
		},
	})
	require.NoError(t, err)
	assert.NotNil(t, s)

	_, ok := s.(*kodoStorage)
	assert.True(t, ok)
}

func TestNew_UnsupportedDriver(t *testing.T) {
	_, err := New(Config{
		Driver: "s3",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported driver")
	assert.Contains(t, err.Error(), "supported: oss, cos, kodo")
}

func TestNew_EmptyDriver(t *testing.T) {
	_, err := New(Config{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported driver")
}

func TestNew_OSSWithNilConfig(t *testing.T) {
	// Driver 为 OSS 但 OSS 配置为 nil
	_, err := New(Config{
		Driver: DriverOSS,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "OSS config is nil")
}

func TestNew_COSWithNilConfig(t *testing.T) {
	_, err := New(Config{
		Driver: DriverCOS,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "COS config is nil")
}

func TestNew_KODOWithNilConfig(t *testing.T) {
	_, err := New(Config{
		Driver: DriverKODO,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "KODO config is nil")
}

func TestMustNew_Success(t *testing.T) {
	s := MustNew(Config{
		Driver: DriverOSS,
		OSS: &OSSConfig{
			Endpoint:        "oss-cn-hangzhou.aliyuncs.com",
			AccessKeyID:     "test-access-key-id",
			AccessKeySecret: "test-access-key-secret",
			Bucket:          "test-bucket",
		},
	})
	assert.NotNil(t, s)
}

func TestMustNew_Panic(t *testing.T) {
	assert.Panics(t, func() {
		MustNew(Config{Driver: "unsupported"})
	})
}

func TestMustNew_PanicOnNilConfig(t *testing.T) {
	assert.Panics(t, func() {
		MustNew(Config{Driver: DriverOSS})
	})
}
