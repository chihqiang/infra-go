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

// --- HMAC hash functions ---

// HMACSHA1 computes the HMAC-SHA1 of data with a key and returns a hex string.
func HMACSHA1(key, data []byte) string {
	return hmacHex(sha1.New, key, data)
}

// HMACSHA1String computes the HMAC-SHA1 of a string with a key.
func HMACSHA1String(key, s string) string {
	return HMACSHA1([]byte(key), []byte(s))
}

// HMACSHA256 computes the HMAC-SHA256 of data with a key and returns a hex string.
func HMACSHA256(key, data []byte) string {
	return hmacHex(sha256.New, key, data)
}

// HMACSHA256String computes the HMAC-SHA256 of a string with a key.
func HMACSHA256String(key, s string) string {
	return HMACSHA256([]byte(key), []byte(s))
}

// HMACSHA512 computes the HMAC-SHA512 of data with a key and returns a hex string.
func HMACSHA512(key, data []byte) string {
	return hmacHex(sha512.New, key, data)
}

// HMACSHA512String computes the HMAC-SHA512 of a string with a key.
func HMACSHA512String(key, s string) string {
	return HMACSHA512([]byte(key), []byte(s))
}

// HMACSHA3_256 computes the HMAC-SHA3-256 of data with a key and returns a hex string.
func HMACSHA3_256(key, data []byte) string {
	return hmacHex(func() hash.Hash { return sha3.New256() }, key, data)
}

// HMACSHA3_512 computes the HMAC-SHA3-512 of data with a key and returns a hex string.
func HMACSHA3_512(key, data []byte) string {
	return hmacHex(func() hash.Hash { return sha3.New512() }, key, data)
}

// --- generic HMAC ---

// HMAC computes the HMAC of data with the given hash function and key and returns a byte
// slice. hashFunc is a hash constructor, e.g. sha256.New.
func HMAC(hashFunc func() hash.Hash, key, data []byte) []byte {
	h := hmac.New(hashFunc, key)
	h.Write(data)
	return h.Sum(nil)
}

// HMACHex computes the HMAC of data with the given hash function and key and returns a
// hex string.
func HMACHex(hashFunc func() hash.Hash, key, data []byte) string {
	return hex.EncodeToString(HMAC(hashFunc, key, data))
}

// --- verification ---

// Equal reports whether two hash values are equal (constant-time comparison, preventing
// timing attacks).
func Equal(a, b []byte) bool {
	return hmac.Equal(a, b)
}

// EqualHex reports whether two hex hash strings are equal (constant-time comparison).
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

// --- internal helpers ---

// hmacHex computes the HMAC of data with the given hash function and key and returns a
// hex string.
func hmacHex(hashFunc func() hash.Hash, key, data []byte) string {
	return hex.EncodeToString(HMAC(hashFunc, key, data))
}
