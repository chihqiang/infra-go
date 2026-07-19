package hash

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"hash"

	"golang.org/x/crypto/sha3"
)

// --- HMAC 哈希函数 ---

// HMACSHA1 使用密钥计算数据的 HMAC-SHA1，返回十六进制字符串。
func HMACSHA1(key, data []byte) string {
	return hmacHex(sha1.New, key, data)
}

// HMACSHA1String 使用密钥计算字符串的 HMAC-SHA1。
func HMACSHA1String(key, s string) string {
	return HMACSHA1([]byte(key), []byte(s))
}

// HMACSHA256 使用密钥计算数据的 HMAC-SHA256，返回十六进制字符串。
func HMACSHA256(key, data []byte) string {
	return hmacHex(sha256.New, key, data)
}

// HMACSHA256String 使用密钥计算字符串的 HMAC-SHA256。
func HMACSHA256String(key, s string) string {
	return HMACSHA256([]byte(key), []byte(s))
}

// HMACSHA512 使用密钥计算数据的 HMAC-SHA512，返回十六进制字符串。
func HMACSHA512(key, data []byte) string {
	return hmacHex(sha512.New, key, data)
}

// HMACSHA512String 使用密钥计算字符串的 HMAC-SHA512。
func HMACSHA512String(key, s string) string {
	return HMACSHA512([]byte(key), []byte(s))
}

// HMACSHA3_256 使用密钥计算数据的 HMAC-SHA3-256，返回十六进制字符串。
func HMACSHA3_256(key, data []byte) string {
	return hmacHex(func() hash.Hash { return sha3.New256() }, key, data)
}

// HMACSHA3_512 使用密钥计算数据的 HMAC-SHA3-512，返回十六进制字符串。
func HMACSHA3_512(key, data []byte) string {
	return hmacHex(func() hash.Hash { return sha3.New512() }, key, data)
}

// --- 通用 HMAC ---

// HMAC 使用指定的哈希函数和密钥计算数据的 HMAC，返回字节数组。
// hashFunc 为哈希函数构造器，如 sha256.New。
func HMAC(hashFunc func() hash.Hash, key, data []byte) []byte {
	h := hmac.New(hashFunc, key)
	h.Write(data)
	return h.Sum(nil)
}

// HMACHex 使用指定的哈希函数和密钥计算数据的 HMAC，返回十六进制字符串。
func HMACHex(hashFunc func() hash.Hash, key, data []byte) string {
	return hex.EncodeToString(HMAC(hashFunc, key, data))
}

// --- 验证 ---

// Equal 比较两个哈希值是否相等（使用恒定时间比较，防止时序攻击）。
func Equal(a, b []byte) bool {
	return hmac.Equal(a, b)
}

// EqualHex 比较两个十六进制哈希字符串是否相等（使用恒定时间比较）。
func EqualHex(a, b string) bool {
	aBytes, err := hex.DecodeString(a)
	if err != nil {
		return false
	}
	bBytes, err := hex.DecodeString(b)
	if err != nil {
		return false
	}
	return hmac.Equal(aBytes, bBytes)
}

// --- 内部辅助 ---

// hmacHex 使用指定的哈希函数和密钥计算数据的 HMAC，返回十六进制字符串。
func hmacHex(hashFunc func() hash.Hash, key, data []byte) string {
	return hex.EncodeToString(HMAC(hashFunc, key, data))
}
