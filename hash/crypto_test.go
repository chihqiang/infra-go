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
	enc, err := AESGCMEncrypt(testAESKey, []byte("secret"))
	require.NoError(t, err)

	// 篡改密文（翻转中间一个字符）。
	// 替换字符必须与原文不同：base64 字母表包含 'X'，
	// 若该位置恰好就是 'X'，替换会成为空操作，解密成功导致测试偶发失败。
	i := len(enc) / 2
	replacement := byte('X')
	if enc[i] == replacement {
		replacement = 'Y'
	}
	require.NotEqual(t, enc[i], replacement)

	tampered := enc[:i] + string(replacement) + enc[i+1:]
	require.NotEqual(t, enc, tampered, "tampering must actually change the ciphertext")

	_, err = AESGCMDecrypt(testAESKey, tampered)
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
