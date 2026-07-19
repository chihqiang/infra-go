package hash

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"hash"
	"io"
	"os"

	"golang.org/x/crypto/sha3"
)

// --- 基础哈希函数 ---

// MD5 返回输入数据的 MD5 哈希值（16 字节，32 位十六进制字符串）。
// 注意：MD5 不再安全，不应用于密码存储或安全场景，仅适用于数据校验。
func MD5(data []byte) string {
	sum := md5.Sum(data)
	return hex.EncodeToString(sum[:])
}

// MD5String 返回字符串的 MD5 哈希值。
func MD5String(s string) string {
	return MD5([]byte(s))
}

// SHA1 返回输入数据的 SHA1 哈希值（20 字节，40 位十六进制字符串）。
func SHA1(data []byte) string {
	sum := sha1.Sum(data)
	return hex.EncodeToString(sum[:])
}

// SHA1String 返回字符串的 SHA1 哈希值。
func SHA1String(s string) string {
	return SHA1([]byte(s))
}

// SHA224 返回输入数据的 SHA224 哈希值。
func SHA224(data []byte) string {
	sum := sha256.Sum224(data)
	return hex.EncodeToString(sum[:])
}

// SHA224String 返回字符串的 SHA224 哈希值。
func SHA224String(s string) string {
	return SHA224([]byte(s))
}

// SHA256 返回输入数据的 SHA256 哈希值（32 字节，64 位十六进制字符串）。
func SHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// SHA256String 返回字符串的 SHA256 哈希值。
func SHA256String(s string) string {
	return SHA256([]byte(s))
}

// SHA384 返回输入数据的 SHA384 哈希值。
func SHA384(data []byte) string {
	sum := sha512.Sum384(data)
	return hex.EncodeToString(sum[:])
}

// SHA384String 返回字符串的 SHA384 哈希值。
func SHA384String(s string) string {
	return SHA384([]byte(s))
}

// SHA512 返回输入数据的 SHA512 哈希值（64 字节，128 位十六进制字符串）。
func SHA512(data []byte) string {
	sum := sha512.Sum512(data)
	return hex.EncodeToString(sum[:])
}

// SHA512String 返回字符串的 SHA512 哈希值。
func SHA512String(s string) string {
	return SHA512([]byte(s))
}

// SHA512_224 返回输入数据的 SHA512/224 哈希值。
func SHA512_224(data []byte) string {
	sum := sha512.Sum512_224(data)
	return hex.EncodeToString(sum[:])
}

// SHA512_224String 返回字符串的 SHA512/224 哈希值。
func SHA512_224String(s string) string {
	return SHA512_224([]byte(s))
}

// SHA512_256 返回输入数据的 SHA512/256 哈希值。
func SHA512_256(data []byte) string {
	sum := sha512.Sum512_256(data)
	return hex.EncodeToString(sum[:])
}

// SHA512_256String 返回字符串的 SHA512/256 哈希值。
func SHA512_256String(s string) string {
	return SHA512_256([]byte(s))
}

// --- SHA3 系列 ---

// SHA3_256 返回输入数据的 SHA3-256 哈希值。
func SHA3_256(data []byte) string {
	sum := sha3.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// SHA3_256String 返回字符串的 SHA3-256 哈希值。
func SHA3_256String(s string) string {
	return SHA3_256([]byte(s))
}

// SHA3_512 返回输入数据的 SHA3-512 哈希值。
func SHA3_512(data []byte) string {
	sum := sha3.Sum512(data)
	return hex.EncodeToString(sum[:])
}

// SHA3_512String 返回字符串的 SHA3-512 哈希值。
func SHA3_512String(s string) string {
	return SHA3_512([]byte(s))
}

// --- 文件哈希 ---

// FileMD5 计算文件的 MD5 哈希值。
// 适用于大文件，内部使用流式读取。
func FileMD5(path string) (string, error) {
	return fileHash(path, md5.New())
}

// FileSHA1 计算文件的 SHA1 哈希值。
func FileSHA1(path string) (string, error) {
	return fileHash(path, sha1.New())
}

// FileSHA256 计算文件的 SHA256 哈希值。
func FileSHA256(path string) (string, error) {
	return fileHash(path, sha256.New())
}

// FileSHA512 计算文件的 SHA512 哈希值。
func FileSHA512(path string) (string, error) {
	return fileHash(path, sha512.New())
}

// fileHash 使用指定的 hash.Hash 计算文件哈希值。
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

// --- 通用哈希接口 ---

// Hash 使用指定的 hash.Hash 计算数据的哈希值，返回十六进制字符串。
// 适用于需要自定义哈希算法的场景。
func Hash(data []byte, h hash.Hash) string {
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

// --- 编码辅助 ---

// HexEncode 将字节数组编码为十六进制字符串。
func HexEncode(data []byte) string {
	return hex.EncodeToString(data)
}

// HexDecode 将十六进制字符串解码为字节数组。
func HexDecode(s string) ([]byte, error) {
	return hex.DecodeString(s)
}

// --- 错误定义 ---

// ErrEmptyData 表示输入数据为空。
var ErrEmptyData = errors.New("hash: input data is empty")
