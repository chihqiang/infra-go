package hash

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
)

// ErrInvalidAESKey reports an illegal AES key length.
var ErrInvalidAESKey = errors.New("hash: invalid AES key, must be 16/24/32 bytes")

// ErrDecryptFailed reports a failed decryption (wrong key or tampered data).
var ErrDecryptFailed = errors.New("hash: decrypt failed or data tampered")

// --- AES-GCM symmetric encryption ---

// AESGCMEncrypt encrypts data with AES-GCM and returns a base64-encoded ciphertext.
// The key must be 16/24/32 bytes long (corresponding to AES-128/192/256).
//
// Return format: base64( nonce || ciphertext ), where nonce is a randomly generated
// 12-byte value. AES-GCM is authenticated encryption (AEAD), guaranteeing both
// confidentiality and integrity (tamper resistance).
func AESGCMEncrypt(key, plaintext []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", ErrInvalidAESKey
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	// Seal appends the authentication tag to the ciphertext
	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// AESGCMDecrypt decrypts base64-encoded AES-GCM ciphertext and verifies the
// authentication tag (tamper resistance). The key must be the same one used for
// encryption; a wrong key or tampered data returns an error.
func AESGCMDecrypt(key []byte, encoded string) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrInvalidAESKey
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}
	if len(data) < gcm.NonceSize() {
		return nil, ErrDecryptFailed
	}

	nonce, ciphertext := data[:gcm.NonceSize()], data[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, ErrDecryptFailed
	}
	return plaintext, nil
}

// --- HMAC signing ---

// HMACSign computes an HMAC-SHA256 signature over data with the given key and returns a
// base64-encoded string. It is commonly used for request signing / tamper checking.
func HMACSign(key []byte, data string) string {
	h := hmac.New(sha256.New, key)
	_, _ = h.Write([]byte(data))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// HMACVerify verifies whether the HMAC-SHA256 signature of data matches.
// It uses hmac.Equal for a constant-time comparison, preventing timing attacks.
func HMACVerify(key []byte, data, signature string) bool {
	expected := HMACSign(key, data)
	return hmac.Equal([]byte(expected), []byte(signature))
}
