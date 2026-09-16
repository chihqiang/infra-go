package stringx

import (
	crand "crypto/rand"
	"encoding/hex"
	"math/rand"
	"sync"
	"time"
)

// RandType defines the kind of random string to generate.
type RandType int

const (
	RandTypeAll   RandType = iota // all: upper + lower case letters and digits
	RandTypeUpper                 // upper case letters only
	RandTypeLower                 // lower case letters only
	RandTypeDigit                 // digits only
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
	digitIdxMask     = 1<<digitIdxBits - 1  // rejects values >= 10
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
	// A negative or zero length returns an empty string right away, avoiding a
	// make([]byte, n) panic.
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

	// The digit alphabet holds only 10 characters, which a 4-bit index can
	// represent (rejecting values >= 10); compared with the generic 6-bit scheme
	// this wastes fewer random bits.
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
