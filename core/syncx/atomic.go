package syncx

import (
	"encoding/binary"
	"fmt"
	"hash/maphash"
	"math"
)

// hashSeed is a global maphash seed, fixed for the lifetime of the process.
// maphash needs a fixed seed so hash results stay consistent (they still differ
// between runs).
var hashSeed = maphash.MakeSeed()

// hashKey computes a hash value for any comparable type.
// Common types (string, the int family, the uint family, the float family) take
// a fast path that avoids the reflection overhead of fmt.Sprint; every other
// type falls back to fmt.Sprint.
func hashKey[K comparable](key K) uint64 {
	var h maphash.Hash
	h.SetSeed(hashSeed)

	switch v := any(key).(type) {
	case string:
		h.WriteString(v)
	case int:
		var buf [8]byte
		binary.LittleEndian.PutUint64(buf[:], uint64(v))
		h.Write(buf[:])
	case int8:
		var buf [8]byte
		buf[0] = byte(v)
		h.Write(buf[:1])
	case int16:
		var buf [8]byte
		binary.LittleEndian.PutUint16(buf[:], uint16(v))
		h.Write(buf[:2])
	case int32:
		var buf [8]byte
		binary.LittleEndian.PutUint32(buf[:], uint32(v))
		h.Write(buf[:4])
	case int64:
		var buf [8]byte
		binary.LittleEndian.PutUint64(buf[:], uint64(v))
		h.Write(buf[:])
	case uint:
		var buf [8]byte
		binary.LittleEndian.PutUint64(buf[:], uint64(v))
		h.Write(buf[:])
	case uint8:
		var buf [8]byte
		buf[0] = v
		h.Write(buf[:1])
	case uint16:
		var buf [8]byte
		binary.LittleEndian.PutUint16(buf[:], v)
		h.Write(buf[:2])
	case uint32:
		var buf [8]byte
		binary.LittleEndian.PutUint32(buf[:], v)
		h.Write(buf[:4])
	case uint64:
		var buf [8]byte
		binary.LittleEndian.PutUint64(buf[:], v)
		h.Write(buf[:])
	case float32:
		var buf [4]byte
		binary.LittleEndian.PutUint32(buf[:], math.Float32bits(v))
		h.Write(buf[:])
	case float64:
		var buf [8]byte
		binary.LittleEndian.PutUint64(buf[:], math.Float64bits(v))
		h.Write(buf[:])
	case bool:
		if v {
			h.WriteByte(1)
		} else {
			h.WriteByte(0)
		}
	default:
		// Going through an any type assertion supports complex comparable types
		// such as structs.
		h.WriteString(fmt.Sprint(v))
	}

	return h.Sum64()
}
