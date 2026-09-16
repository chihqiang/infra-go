package hash

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testAESKey = []byte("0123456789abcdef") // 16 bytes, AES-128

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
	assert.NotEqual(t, e1, e2, "the nonce is random each time, so identical plaintext must differ")
}

func TestAESGCMDecrypt_Tampered(t *testing.T) {
	enc, err := AESGCMEncrypt(testAESKey, []byte("secret"))
	require.NoError(t, err)

	// Tamper with the ciphertext (flip a character in the middle).
	// The replacement character must differ from the original: the base64 alphabet
	// contains 'X', so if that position happens to be 'X' the replacement becomes a no-op,
	// decryption succeeds and the test fails intermittently.
	i := len(enc) / 2
	replacement := byte('X')
	if enc[i] == replacement {
		replacement = 'Y'
	}
	require.NotEqual(t, enc[i], replacement)

	tampered := enc[:i] + string(replacement) + enc[i+1:]
	require.NotEqual(t, enc, tampered, "tampering must actually change the ciphertext")

	_, err = AESGCMDecrypt(testAESKey, tampered)
	assert.Error(t, err, "tampered ciphertext must fail verification")
}

func TestAESGCMDecrypt_WrongKey(t *testing.T) {
	enc, _ := AESGCMEncrypt(testAESKey, []byte("secret"))
	_, err := AESGCMDecrypt([]byte("abcdefghijklmnop"), enc)
	assert.Error(t, err, "a wrong key must fail decryption")
}

func TestAESGCM_InvalidKey(t *testing.T) {
	_, err := AESGCMEncrypt([]byte("short"), []byte("x"))
	assert.ErrorIs(t, err, ErrInvalidAESKey)
}

// --- HMAC signing ---

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
