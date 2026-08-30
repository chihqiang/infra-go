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

// ErrInvalidAESKey AES 密钥长度非法。
var ErrInvalidAESKey = errors.New("hash: invalid AES key, must be 16/24/32 bytes")

// ErrDecryptFailed 解密失败（密钥错误或数据被篡改）。
var ErrDecryptFailed = errors.New("hash: decrypt failed or data tampered")

// --- AES-GCM 对称加密 ---

// AESGCMEncrypt 使用 AES-GCM 加密数据，返回 base64 编码的密文。
// 密钥 key 长度必须为 16/24/32 字节（对应 AES-128/192/256）。
//
// 返回格式：base64( nonce || ciphertext )，nonce 为每次随机生成的 12 字节。
// AES-GCM 是认证加密（AEAD），同时保证机密性与完整性（防篡改）。
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

	// Seal 追加认证标签到密文后
	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// AESGCMDecrypt 解密 base64 编码的 AES-GCM 密文，校验认证标签（防篡改）。
// 密钥必须与加密时一致；密钥错误或数据被篡改时返回错误。
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

// --- HMAC 签名 ---

// HMACSign 使用密钥对数据计算 HMAC-SHA256 签名，返回 base64 编码字符串。
// 常用于请求签名/防篡改校验。
func HMACSign(key []byte, data string) string {
	h := hmac.New(sha256.New, key)
	_, _ = h.Write([]byte(data))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// HMACVerify 校验数据的 HMAC-SHA256 签名是否匹配。
// 使用 hmac.Equal 常量时间比较，防止时序攻击。
func HMACVerify(key []byte, data, signature string) bool {
	expected := HMACSign(key, data)
	return hmac.Equal([]byte(expected), []byte(signature))
}
