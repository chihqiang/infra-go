package stringx

import (
	crand "crypto/rand"
	"encoding/hex"
	"math/rand"
	"sync"
	"time"
)

// RandType 定义随机字符串类型
type RandType int

const (
	RandTypeAll   RandType = iota // 全部：大小写 + 数字
	RandTypeUpper                 // 仅大写字母
	RandTypeLower                 // 仅小写字母
	RandTypeDigit                 // 仅数字
)

const (
	letterBytesUpper = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	letterBytesLower = "abcdefghijklmnopqrstuvwxyz"
	letterBytesDigit = "0123456789"
	letterBytes      = letterBytesLower + letterBytesUpper + letterBytesDigit
	letterIdxBits    = 6 // 6 bits to represent a letter index
	idLen            = 8
	defaultRandLen   = 8
	letterIdxMask    = 1<<letterIdxBits - 1 // All 1-bits, as many as letterIdxBits
	letterIdxMax     = 63 / letterIdxBits   // # of letter indices fitting in 63 bits
	digitIdxBits     = 4                    // 4 bits to represent a digit index (0-9)
	digitIdxMask     = 1<<digitIdxBits - 1  // 拒绝 >= 10 的值
	digitIdxMax      = 63 / digitIdxBits    // # of digit indices fitting in 63 bits
)

var src = newLockedSource(time.Now().UnixNano())

type lockedSource struct {
	source rand.Source
	lock   sync.Mutex
}

func newLockedSource(seed int64) *lockedSource {
	return &lockedSource{
		source: rand.NewSource(seed),
	}
}

func (ls *lockedSource) Int63() int64 {
	ls.lock.Lock()
	defer ls.lock.Unlock()
	return ls.source.Int63()
}

func (ls *lockedSource) Seed(seed int64) {
	ls.lock.Lock()
	defer ls.lock.Unlock()
	ls.source.Seed(seed)
}

// Rand returns a random string.
func Rand() string {
	return Randn(defaultRandLen, RandTypeAll)
}

// RandId returns a random id string.
func RandId() string {
	b := make([]byte, idLen)
	_, err := crand.Read(b)
	if err != nil {
		return Randn(idLen, RandTypeAll)
	}

	return hex.EncodeToString(b)
}

// Randn returns a random string with length n and specified type.
func Randn(n int, randType RandType) string {
	// 负数或零长度直接返回空串，避免 make([]byte, n) panic。
	if n <= 0 {
		return ""
	}
	var chars string
	switch randType {
	case RandTypeUpper:
		chars = letterBytesUpper
	case RandTypeLower:
		chars = letterBytesLower
	case RandTypeDigit:
		chars = letterBytesDigit
	default:
		chars = letterBytes
	}

	b := make([]byte, n)

	// 数字集合仅 10 个字符，用 4 位索引即可表示（拒绝 >= 10），
	// 相比通用的 6 位方案浪费更少的随机位。
	if randType == RandTypeDigit {
		for i, cache, remain := n-1, src.Int63(), digitIdxMax; i >= 0; {
			if remain == 0 {
				cache, remain = src.Int63(), digitIdxMax
			}
			if idx := int(cache & digitIdxMask); idx < len(chars) {
				b[i] = chars[idx]
				i--
			}
			cache >>= digitIdxBits
			remain--
		}
		return string(b)
	}

	// A src.Int63() generates 63 random bits, enough for letterIdxMax characters!
	for i, cache, remain := n-1, src.Int63(), letterIdxMax; i >= 0; {
		if remain == 0 {
			cache, remain = src.Int63(), letterIdxMax
		}
		if idx := int(cache & letterIdxMask); idx < len(chars) {
			b[i] = chars[idx]
			i--
		}
		cache >>= letterIdxBits
		remain--
	}

	return string(b)
}

// Seed sets the seed to seed.
func Seed(seed int64) {
	src.Seed(seed)
}
