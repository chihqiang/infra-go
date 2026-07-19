package hash

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- 基础哈希函数测试 ---

func TestMD5(t *testing.T) {
	tests := []struct {
		input  string
		expect string
	}{
		{"", "d41d8cd98f00b204e9800998ecf8427e"},
		{"hello", "5d41402abc4b2a76b9719d911017c592"},
		{"hello world", "5eb63bbbe01eeed093cb22bb8f5acdc3"},
	}

	for _, tt := range tests {
		got := MD5String(tt.input)
		assert.Equal(t, tt.expect, got, "input: %q", tt.input)
	}
}

func TestSHA1(t *testing.T) {
	tests := []struct {
		input  string
		expect string
	}{
		{"", "da39a3ee5e6b4b0d3255bfef95601890afd80709"},
		{"hello", "aaf4c61ddcc5e8a2dabede0f3b482cd9aea9434d"},
		{"hello world", "2aae6c35c94fcfb415dbe95f408b9ce91ee846ed"},
	}

	for _, tt := range tests {
		got := SHA1String(tt.input)
		assert.Equal(t, tt.expect, got, "input: %q", tt.input)
	}
}

func TestSHA224(t *testing.T) {
	got := SHA224String("hello")
	assert.Equal(t, "ea09ae9cc6768c50fcee903ed054556e5bfc8347907f12598aa24193", got)
}

func TestSHA256(t *testing.T) {
	tests := []struct {
		input  string
		expect string
	}{
		{"", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
		{"hello", "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"},
		{"hello world", "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"},
	}

	for _, tt := range tests {
		got := SHA256String(tt.input)
		assert.Equal(t, tt.expect, got, "input: %q", tt.input)
	}
}

func TestSHA384(t *testing.T) {
	got := SHA384String("hello")
	assert.Equal(t, "59e1748777448c69de6b800d7a33bbfb9ff1b463e44354c3553bcdb9c666fa90125a3c79f90397bdf5f6a13de828684f", got)
}

func TestSHA512(t *testing.T) {
	got := SHA512String("hello")
	assert.Equal(t, "9b71d224bd62f3785d96d46ad3ea3d73319bfbc2890caadae2dff72519673ca72323c3d99ba5c11d7c7acc6e14b8c5da0c4663475c2e5c3adef46f73bcdec043", got)
}

func TestSHA512_224(t *testing.T) {
	got := SHA512_224String("hello")
	assert.Len(t, got, 56) // 224 bits = 28 bytes = 56 hex chars
}

func TestSHA512_256(t *testing.T) {
	got := SHA512_256String("hello")
	assert.Len(t, got, 64) // 256 bits = 32 bytes = 64 hex chars
}

func TestSHA3_256(t *testing.T) {
	got := SHA3_256String("hello")
	assert.Equal(t, "3338be694f50c5f338814986cdf0686453a888b84f424d792af4b9202398f392", got)
}

func TestSHA3_512(t *testing.T) {
	got := SHA3_512String("hello")
	assert.Len(t, got, 128) // 512 bits = 64 bytes = 128 hex chars
}

func TestMD5_Bytes(t *testing.T) {
	data := []byte("hello")
	got := MD5(data)
	assert.Equal(t, "5d41402abc4b2a76b9719d911017c592", got)
}

// --- 文件哈希测试 ---

func TestFileMD5(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.txt")
	content := "hello world"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	got, err := FileMD5(path)
	require.NoError(t, err)
	assert.Equal(t, MD5String(content), got)
}

func TestFileSHA256(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.txt")
	content := "hello world"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	got, err := FileSHA256(path)
	require.NoError(t, err)
	assert.Equal(t, SHA256String(content), got)
}

func TestFileSHA1(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.txt")
	content := "hello world"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	got, err := FileSHA1(path)
	require.NoError(t, err)
	assert.Equal(t, SHA1String(content), got)
}

func TestFileSHA512(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.txt")
	content := "hello world"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	got, err := FileSHA512(path)
	require.NoError(t, err)
	assert.Equal(t, SHA512String(content), got)
}

func TestFileMD5_NotExist(t *testing.T) {
	_, err := FileMD5("/nonexistent/file.txt")
	require.Error(t, err)
}

// --- 通用哈希测试 ---

func TestHash(t *testing.T) {
	got := Hash([]byte("hello"), sha256.New())
	assert.Equal(t, SHA256String("hello"), got)
}

// --- 编码辅助测试 ---

func TestHexEncode(t *testing.T) {
	assert.Equal(t, "68656c6c6f", HexEncode([]byte("hello")))
}

func TestHexDecode(t *testing.T) {
	data, err := HexDecode("68656c6c6f")
	require.NoError(t, err)
	assert.Equal(t, "hello", string(data))
}

func TestHexDecode_Error(t *testing.T) {
	_, err := HexDecode("invalid")
	require.Error(t, err)
}

// --- HMAC 测试 ---

func TestHMACSHA256(t *testing.T) {
	key := "secret"
	data := "hello world"
	got := HMACSHA256String(key, data)
	assert.NotEmpty(t, got)

	// 验证确定性
	assert.Equal(t, HMACSHA256String(key, data), got)
}

func TestHMACSHA256_KnownValue(t *testing.T) {
	// 使用已知向量验证正确性
	key := []byte("key")
	data := []byte("The quick brown fox jumps over the lazy dog")
	got := HMACSHA256(key, data)
	assert.Equal(t, "f7bc83f430538424b13298e6aa6fb143ef4d59a14946175997479dbc2d1a3cd8", got)
}

func TestHMACSHA1(t *testing.T) {
	key := []byte("key")
	data := []byte("The quick brown fox jumps over the lazy dog")
	got := HMACSHA1(key, data)
	assert.Equal(t, "de7c9b85b8b78aa6bc8a7a36f70a90701c9db4d9", got)
}

func TestHMACSHA512(t *testing.T) {
	key := []byte("key")
	data := []byte("The quick brown fox jumps over the lazy dog")
	got := HMACSHA512(key, data)
	assert.Len(t, got, 128)
}

func TestHMACSHA3_256(t *testing.T) {
	got := HMACSHA3_256([]byte("key"), []byte("hello"))
	assert.Len(t, got, 64)
}

func TestHMACSHA3_512(t *testing.T) {
	got := HMACSHA3_512([]byte("key"), []byte("hello"))
	assert.Len(t, got, 128)
}

func TestHMAC_Generic(t *testing.T) {
	got := HMAC(sha256.New, []byte("key"), []byte("hello"))
	assert.NotEmpty(t, got)
}

func TestHMACHex_Generic(t *testing.T) {
	got := HMACHex(sha256.New, []byte("key"), []byte("hello"))
	assert.Len(t, got, 64)
}

func TestEqual(t *testing.T) {
	a := []byte{1, 2, 3}
	b := []byte{1, 2, 3}
	c := []byte{1, 2, 4}
	assert.True(t, Equal(a, b))
	assert.False(t, Equal(a, c))
}

func TestEqualHex(t *testing.T) {
	a := "aaf4c61ddcc5e8a2dabede0f3b482cd9aea9434d"
	b := "aaf4c61ddcc5e8a2dabede0f3b482cd9aea9434d"
	c := "aaf4c61ddcc5e8a2dabede0f3b482cd9aea9434e"
	assert.True(t, EqualHex(a, b))
	assert.False(t, EqualHex(a, c))
}

func TestEqualHex_Invalid(t *testing.T) {
	assert.False(t, EqualHex("invalid", "aaf4c61d"))
}

// --- Bcrypt 测试 ---

func TestBcryptHashDefault(t *testing.T) {
	password := "mySecretPassword"
	hashed, err := BcryptHashDefault(password)
	require.NoError(t, err)
	assert.NotEmpty(t, hashed)
	assert.NotEqual(t, password, hashed)
	assert.True(t, BcryptIsHashed(hashed))
}

func TestBcryptHash_CustomCost(t *testing.T) {
	password := "mySecretPassword"
	hashed, err := BcryptHash(password, BcryptCostMin)
	require.NoError(t, err)
	assert.NotEmpty(t, hashed)
	assert.True(t, BcryptIsHashed(hashed))
}

func TestBcryptHash_NegativeCost(t *testing.T) {
	// cost < 0 时使用默认值
	hashed, err := BcryptHash("password", -1)
	require.NoError(t, err)
	assert.NotEmpty(t, hashed)
}

func TestBcryptMatch(t *testing.T) {
	password := "mySecretPassword"
	hashed, err := BcryptHashDefault(password)
	require.NoError(t, err)

	assert.True(t, BcryptMatch(hashed, password))
	assert.False(t, BcryptMatch(hashed, "wrongPassword"))
}

func TestBcryptCompare(t *testing.T) {
	password := "mySecretPassword"
	hashed, err := BcryptHashDefault(password)
	require.NoError(t, err)

	assert.NoError(t, BcryptCompare(hashed, password))
	assert.Error(t, BcryptCompare(hashed, "wrongPassword"))
}

func TestBcryptIsHashed(t *testing.T) {
	tests := []struct {
		input  string
		expect bool
	}{
		{"$2a$10$abcdef", true},
		{"$2b$10$abcdef", true},
		{"$2y$10$abcdef", true},
		{"$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy", true},
		{"not-a-hash", false},
		{"", false},
		{"short", false},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.expect, BcryptIsHashed(tt.input), "input: %q", tt.input)
	}
}

func TestBcryptCostConstants(t *testing.T) {
	assert.Equal(t, 4, BcryptCostMin)
	assert.Equal(t, 31, BcryptCostMax)
	assert.Equal(t, 10, BcryptCostDefault)
}

// --- 一致性测试 ---

func TestMD5_FileVsString(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.txt")
	content := "consistency test"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	fileHash, err := FileMD5(path)
	require.NoError(t, err)
	assert.Equal(t, MD5String(content), fileHash)
}

func TestSHA256_FileVsString(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.txt")
	content := "consistency test for sha256"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	fileHash, err := FileSHA256(path)
	require.NoError(t, err)
	assert.Equal(t, SHA256String(content), fileHash)
}
