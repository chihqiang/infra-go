package syncx

import (
	"fmt"
	"hash/fnv"
)

// hashKey 对任意 comparable 类型计算 hash 值。
func hashKey[K comparable](key K) uint64 {
	h := fnv.New64a()
	h.Write([]byte(fmt.Sprint(key)))
	return h.Sum64()
}
