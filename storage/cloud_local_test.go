package storage

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 本文件覆盖云存储（OSS/COS/KODO）中不依赖真实云服务即可验证的本地分支：
// ctx 快速失败、凭证生成（uploadToken）、URL 解析回退等。
// 真实的上传/删除走云 SDK 需真实环境，不在本地测试范围内。

// --- OSS ctx 快速失败 ---

func TestOSS_WriteCancelledCtx(t *testing.T) {
	s, err := NewOSS(&OSSConfig{
		Endpoint:        "oss-cn-hangzhou.aliyuncs.com",
		AccessKeyID:     "test-access-key-id",
		AccessKeySecret: "test-access-key-secret",
		Bucket:          "test-bucket",
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = s.Write(ctx, "a.txt", []byte("x"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "write OSS object")
}

func TestOSS_DeleteCancelledCtx(t *testing.T) {
	s, err := NewOSS(&OSSConfig{
		Endpoint:        "oss-cn-hangzhou.aliyuncs.com",
		AccessKeyID:     "test-access-key-id",
		AccessKeySecret: "test-access-key-secret",
		Bucket:          "test-bucket",
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = s.Delete(ctx, "a.txt")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "delete OSS object")
}

// --- resolveOSSURL 解析 ---

func TestResolveOSSURL_Default(t *testing.T) {
	// 无 URL、endpoint 无协议 → 默认 https://bucket.endpoint
	got := resolveOSSURL(&OSSConfig{
		Endpoint: "oss-cn-hangzhou.aliyuncs.com",
		Bucket:   "bkt",
	})
	assert.Equal(t, "https://bkt.oss-cn-hangzhou.aliyuncs.com", got)
}

func TestResolveOSSURL_CustomURL(t *testing.T) {
	got := resolveOSSURL(&OSSConfig{
		Endpoint: "oss-cn-hangzhou.aliyuncs.com",
		Bucket:   "bkt",
		URL:      "https://cdn.example.com",
	})
	assert.Equal(t, "https://cdn.example.com", got)
}

// --- COS ctx 快速失败 ---

func TestCOS_WriteCancelledCtx(t *testing.T) {
	s, err := NewCOS(&COSConfig{
		BucketURL: "https://test-bucket.cos.ap-beijing.myqcloud.com",
		SecretID:  "test-secret-id",
		SecretKey: "test-secret-key",
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = s.Write(ctx, "a.txt", []byte("x"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "write COS object")
}

func TestCOS_DeleteCancelledCtx(t *testing.T) {
	s, err := NewCOS(&COSConfig{
		BucketURL: "https://test-bucket.cos.ap-beijing.myqcloud.com",
		SecretID:  "test-secret-id",
		SecretKey: "test-secret-key",
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = s.Delete(ctx, "a.txt")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "delete COS object")
}

// --- KODO 本地分支 ---

func TestKODO_UploadToken(t *testing.T) {
	s, err := NewKODO(&KODOConfig{
		AccessKey: "test-access-key",
		SecretKey: "test-secret-key",
		Bucket:    "test-bucket",
	})
	require.NoError(t, err)

	ks, ok := s.(*kodoStorage)
	require.True(t, ok)
	// 上传凭证应非空（纯本地 HMAC 签名）
	assert.NotEmpty(t, ks.uploadToken())
}

func TestKODO_WriteCancelledCtx(t *testing.T) {
	s, err := NewKODO(&KODOConfig{
		AccessKey: "test-access-key",
		SecretKey: "test-secret-key",
		Bucket:    "test-bucket",
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = s.Write(ctx, "a.txt", []byte("x"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "write KODO object")
}

func TestKODO_DeleteCancelledCtx(t *testing.T) {
	s, err := NewKODO(&KODOConfig{
		AccessKey: "test-access-key",
		SecretKey: "test-secret-key",
		Bucket:    "test-bucket",
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = s.Delete(ctx, "a.txt")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "delete KODO object")
}
