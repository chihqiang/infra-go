package hash

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"hash"
	"io"
	"os"

	"golang.org/x/crypto/sha3"
)

// --- basic hash functions ---

// MD5 returns the MD5 hash of the input data (16 bytes, 32 hex characters).
// Note: MD5 is no longer secure and should not be used for password storage or security
// purposes; it is suitable only for data checksums.
func MD5(data []byte) string {
	sum := md5.Sum(data)
	return hex.EncodeToString(sum[:])
}

// MD5String returns the MD5 hash of a string.
func MD5String(s string) string {
	return MD5([]byte(s))
}

// SHA1 returns the SHA1 hash of the input data (20 bytes, 40 hex characters).
func SHA1(data []byte) string {
	sum := sha1.Sum(data)
	return hex.EncodeToString(sum[:])
}

// SHA1String returns the SHA1 hash of a string.
func SHA1String(s string) string {
	return SHA1([]byte(s))
}

// SHA224 returns the SHA224 hash of the input data.
func SHA224(data []byte) string {
	sum := sha256.Sum224(data)
	return hex.EncodeToString(sum[:])
}

// SHA224String returns the SHA224 hash of a string.
func SHA224String(s string) string {
	return SHA224([]byte(s))
}

// SHA256 returns the SHA256 hash of the input data (32 bytes, 64 hex characters).
func SHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// SHA256String returns the SHA256 hash of a string.
func SHA256String(s string) string {
	return SHA256([]byte(s))
}

// SHA384 returns the SHA384 hash of the input data.
func SHA384(data []byte) string {
	sum := sha512.Sum384(data)
	return hex.EncodeToString(sum[:])
}

// SHA384String returns the SHA384 hash of a string.
func SHA384String(s string) string {
	return SHA384([]byte(s))
}

// SHA512 returns the SHA512 hash of the input data (64 bytes, 128 hex characters).
func SHA512(data []byte) string {
	sum := sha512.Sum512(data)
	return hex.EncodeToString(sum[:])
}

// SHA512String returns the SHA512 hash of a string.
func SHA512String(s string) string {
	return SHA512([]byte(s))
}

// SHA512_224 returns the SHA512/224 hash of the input data.
func SHA512_224(data []byte) string {
	sum := sha512.Sum512_224(data)
	return hex.EncodeToString(sum[:])
}

// SHA512_224String returns the SHA512/224 hash of a string.
func SHA512_224String(s string) string {
	return SHA512_224([]byte(s))
}

// SHA512_256 returns the SHA512/256 hash of the input data.
func SHA512_256(data []byte) string {
	sum := sha512.Sum512_256(data)
	return hex.EncodeToString(sum[:])
}

// SHA512_256String returns the SHA512/256 hash of a string.
func SHA512_256String(s string) string {
	return SHA512_256([]byte(s))
}

// --- SHA3 family ---

// SHA3_256 returns the SHA3-256 hash of the input data.
func SHA3_256(data []byte) string {
	sum := sha3.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// SHA3_256String returns the SHA3-256 hash of a string.
func SHA3_256String(s string) string {
	return SHA3_256([]byte(s))
}

// SHA3_512 returns the SHA3-512 hash of the input data.
func SHA3_512(data []byte) string {
	sum := sha3.Sum512(data)
	return hex.EncodeToString(sum[:])
}

// SHA3_512String returns the SHA3-512 hash of a string.
func SHA3_512String(s string) string {
	return SHA3_512([]byte(s))
}

// --- file hashing ---

// FileMD5 computes the MD5 hash of a file.
// It suits large files and reads the content in a streaming fashion internally.
func FileMD5(path string) (string, error) {
	return fileHash(path, md5.New())
}

// FileSHA1 computes the SHA1 hash of a file.
func FileSHA1(path string) (string, error) {
	return fileHash(path, sha1.New())
}

// FileSHA256 computes the SHA256 hash of a file.
func FileSHA256(path string) (string, error) {
	return fileHash(path, sha256.New())
}

// FileSHA512 computes the SHA512 hash of a file.
func FileSHA512(path string) (string, error) {
	return fileHash(path, sha512.New())
}

// fileHash computes a file hash using the given hash.Hash.
func fileHash(path string, h hash.Hash) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()

	if _, err := io.Copy(h, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// --- generic hash interface ---

// Hash computes the hash of data with the given hash.Hash and returns a hex string.
// It is intended for cases that need a custom hash algorithm.
//
// h is Reset first, so reusing a single hash.Hash instance (such as a package-level
// shared sha256.New()) is safe: every call evaluates only the current data.
// Note: this function resets h's internal state, so do not use it to compute an
// incrementally accumulated digest (for accumulation use h.Write / h.Sum directly).
func Hash(data []byte, h hash.Hash) string {
	// Write does not reset the state and neither does Sum. Without the Reset, a second
	// call on a reused instance would return a "cumulative digest" instead of the digest
	// of the current data, and it would not report any error.
	h.Reset()
	_, _ = h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

// --- encoding helpers ---

// HexEncode encodes a byte slice as a hex string.
func HexEncode(data []byte) string {
	return hex.EncodeToString(data)
}

// HexDecode decodes a hex string into a byte slice.
func HexDecode(s string) ([]byte, error) {
	return hex.DecodeString(s)
}
