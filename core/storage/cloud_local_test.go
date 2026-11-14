package storage

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file covers the local branches of the cloud storages (OSS/COS/KODO) that
// can be verified without a real cloud service: ctx fast-fail, credential
// generation (uploadToken), URL resolution fallback, and so on.
// Real uploads/deletes go through the cloud SDKs and need a real environment, so
// they are out of scope for local tests.

// --- OSS ctx fast-fail ---

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

// --- resolveOSSURL resolution ---

func TestResolveOSSURL_Default(t *testing.T) {
	// No URL and an endpoint without a protocol -> default https://bucket.endpoint
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

// --- COS ctx fast-fail ---

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

// --- KODO local branches ---

func TestKODO_UploadToken(t *testing.T) {
	s, err := NewKODO(&KODOConfig{
		AccessKey: "test-access-key",
		SecretKey: "test-secret-key",
		Bucket:    "test-bucket",
	})
	require.NoError(t, err)

	ks, ok := s.(*kodoStorage)
	require.True(t, ok)
	// The upload credential must be non-empty (pure local HMAC signature)
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

// --- OSS Read/Exists ctx fast-fail ---

func TestOSS_ReadCancelledCtx(t *testing.T) {
	s, err := NewOSS(&OSSConfig{
		Endpoint:        "oss-cn-hangzhou.aliyuncs.com",
		AccessKeyID:     "test-access-key-id",
		AccessKeySecret: "test-access-key-secret",
		Bucket:          "test-bucket",
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = s.Read(ctx, "a.txt")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "read OSS object")
}

func TestOSS_ExistsCancelledCtx(t *testing.T) {
	s, err := NewOSS(&OSSConfig{
		Endpoint:        "oss-cn-hangzhou.aliyuncs.com",
		AccessKeyID:     "test-access-key-id",
		AccessKeySecret: "test-access-key-secret",
		Bucket:          "test-bucket",
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = s.Exists(ctx, "a.txt")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "check OSS object")
}

// --- COS Read/Exists ctx fast-fail ---

func TestCOS_ReadCancelledCtx(t *testing.T) {
	s, err := NewCOS(&COSConfig{
		BucketURL: "https://test-bucket.cos.ap-beijing.myqcloud.com",
		SecretID:  "test-secret-id",
		SecretKey: "test-secret-key",
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = s.Read(ctx, "a.txt")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "read COS object")
}

func TestCOS_ExistsCancelledCtx(t *testing.T) {
	s, err := NewCOS(&COSConfig{
		BucketURL: "https://test-bucket.cos.ap-beijing.myqcloud.com",
		SecretID:  "test-secret-id",
		SecretKey: "test-secret-key",
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = s.Exists(ctx, "a.txt")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "check COS object")
}

// --- KODO Read/Exists local branches ---

func TestKODO_ReadCancelledCtx(t *testing.T) {
	s, err := NewKODO(&KODOConfig{
		AccessKey: "test-access-key",
		SecretKey: "test-secret-key",
		Bucket:    "test-bucket",
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = s.Read(ctx, "a.txt")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "read KODO object")
}

func TestKODO_ExistsCancelledCtx(t *testing.T) {
	s, err := NewKODO(&KODOConfig{
		AccessKey: "test-access-key",
		SecretKey: "test-secret-key",
		Bucket:    "test-bucket",
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = s.Exists(ctx, "a.txt")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "check KODO object")
}

func TestKODO_ReadURLNotSet(t *testing.T) {
	// No public access domain in URL: Read must fail before any network request
	s, err := NewKODO(&KODOConfig{
		AccessKey: "test-access-key",
		SecretKey: "test-secret-key",
		Bucket:    "test-bucket",
	})
	require.NoError(t, err)

	_, err = s.Read(context.Background(), "a.txt")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "KODO URL is empty")
}

// --- Cloud driver HTTP status code semantics (httptest stands in for the real cloud service) ---

// TestCOS_DeleteAccepts204 verifies that a COS delete returning 204 No Content is
// treated as success rather than a failure (historical defect: only == 200 was checked).
func TestCOS_DeleteAccepts204(t *testing.T) {
	var gotMethod, gotPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(ts.Close)

	s, err := NewCOS(&COSConfig{BucketURL: ts.URL, SecretID: "test-id", SecretKey: "test-key"})
	require.NoError(t, err)

	n, err := s.Delete(context.Background(), "dir/a.txt")
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)
	assert.Equal(t, http.MethodDelete, gotMethod)
	assert.True(t, strings.HasSuffix(gotPath, "dir/a.txt"), "unexpected path: %q", gotPath)
}

// TestCOS_DeletePropagatesServerError verifies that a 5xx is still reported as an error.
func TestCOS_DeletePropagatesServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(ts.Close)

	s, err := NewCOS(&COSConfig{BucketURL: ts.URL, SecretID: "test-id", SecretKey: "test-key"})
	require.NoError(t, err)

	_, err = s.Delete(context.Background(), "a.txt")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "delete COS object")
}

// TestKODO_ReadAcceptsAny2xx verifies that a KODO read accepts any 2xx
// (for example 206 returned by a CDN).
func TestKODO_ReadAcceptsAny2xx(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte("partial"))
	}))
	t.Cleanup(ts.Close)

	s := &kodoStorage{url: ts.URL}
	data, err := s.Read(context.Background(), "a.txt")
	require.NoError(t, err)
	assert.Equal(t, "partial", string(data))
}

// TestKODO_ReadRejects4xx verifies that a KODO read still rejects 4xx.
func TestKODO_ReadRejects4xx(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(ts.Close)

	s := &kodoStorage{url: ts.URL}
	_, err := s.Read(context.Background(), "a.txt")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read KODO object")
}
