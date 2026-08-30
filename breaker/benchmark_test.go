package breaker

import (
	"testing"
	"time"
)

// 基准场景：真实配置（40 桶 / 250ms 每桶 / 10s 窗口）。
// 走 newGoogleBreaker + defaultSREConfig 构造路径，覆盖真实初始化。
func realGoogleBreaker() *googleBreaker {
	return newGoogleBreaker(defaultSREConfig())
}

// BenchmarkAcceptEmpty 空窗口（无历史数据）下每次 accept 判定的开销。
// 这是正常流量下的常态路径，也是最需要优化的场景。
func BenchmarkAcceptEmpty(b *testing.B) {
	br := realGoogleBreaker()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = br.accept()
	}
}

// BenchmarkAcceptWithHistory 窗口内有历史数据时每次 accept 判定的开销。
func BenchmarkAcceptWithHistory(b *testing.B) {
	br := realGoogleBreaker()
	for i := 0; i < 1000; i++ {
		br.markSuccess()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = br.accept()
	}
}

// BenchmarkDo 通过 Breaker 接口执行一次完整请求判定的开销。
func BenchmarkDo(b *testing.B) {
	br := NewBreaker(WithName("bench"))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = br.Do(func() error { return nil })
	}
}

// BenchmarkWindowReduce 单独测滑动窗口聚合遍历的开销（history 的核心成本）。
func BenchmarkWindowReduce(b *testing.B) {
	rw := newRollingWindow(buckets, window/buckets)
	for i := 0; i < 1000; i++ {
		rw.add(success)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var r windowResult
		rw.reduce(func(bk *bucket) {
			r.accepts += bk.Success
			r.total += bk.Sum
		})
	}
	_ = time.Millisecond
}
