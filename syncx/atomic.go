package syncx

import (
	"encoding/binary"
	"fmt"
	"hash/maphash"
	"math"
)

// hashSeed 全局 maphash 种子，在进程生命周期内固定。
// maphash 需要固定种子来保证 hash 结果的一致性（但不同次运行间不同）。
var hashSeed = maphash.MakeSeed()

// hashKey 对任意 comparable 类型计算 hash 值。
// 对常见类型（string、int 系列、uint 系列、float 系列）使用高效路径，
// 避免 fmt.Sprint 的反射开销；其他类型回退到 fmt.Sprint。
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
		// 通过 any 类型断言，支持 comparable 结构体等复杂类型
		h.WriteString(fmt.Sprint(v))
	}

	return h.Sum64()
}
