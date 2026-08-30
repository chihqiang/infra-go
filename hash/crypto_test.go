package hash

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testAESKey = []byte("0123456789abcdef") // 16 字节，AES-128

// --- AES-GCM ---

func TestAESGCMEncryptDecrypt(t *testing.T) {
	enc, err := AESGCMEncrypt(testAESKey, []byte("hello world"))
	require.NoError(t, err)

	dec, err := AESGCMDecrypt(testAESKey, enc)
	require.NoError(t, err)
	assert.Equal(t, "hello world", string(dec))
}

func TestAESGCMEncrypt_RandomNonce(t *testing.T) {
	e1, _ := AESGCMEncrypt(testAESKey, []byte("same"))
	e2, _ := AESGCMEncrypt(testAESKey, []byte("same"))
	assert.NotEqual(t, e1, e2, "nonce 每次随机，同一明文密文应不同")
}

func TestAESGCMDecrypt_Tampered(t *testing.T) {
	enc, _ := AESGCMEncrypt(testAESKey, []byte("secret"))
	// 篡改密文（翻转中间一个字符）
	tampered := enc[:len(enc)/2] + "X" + enc[len(enc)/2+1:]
	_, err := AESGCMDecrypt(testAESKey, tampered)
	assert.Error(t, err, "篡改后的密文应校验失败")
}

func TestAESGCMDecrypt_WrongKey(t *testing.T) {
	enc, _ := AESGCMEncrypt(testAESKey, []byte("secret"))
	_, err := AESGCMDecrypt([]byte("abcdefghijklmnop"), enc)
	assert.Error(t, err, "错误密钥应解密失败")
}

func TestAESGCM_InvalidKey(t *testing.T) {
	_, err := AESGCMEncrypt([]byte("short"), []byte("x"))
	assert.ErrorIs(t, err, ErrInvalidAESKey)
}

// --- HMAC 签名 ---

func TestHMACSignVerify(t *testing.T) {
	sig := HMACSign(testAESKey, "hello")
	assert.True(t, HMACVerify(testAESKey, "hello", sig))
}

func TestHMACVerify_WrongData(t *testing.T) {
	sig := HMACSign(testAESKey, "hello")
	assert.False(t, HMACVerify(testAESKey, "hell0", sig))
}

func TestHMACVerify_WrongKey(t *testing.T) {
	sig := HMACSign(testAESKey, "hello")
	assert.False(t, HMACVerify([]byte("other-key-123456"), "hello", sig))
}
