package breaker

import (
	"testing"
)

// Benchmark scenario: the real configuration (40 buckets / 250ms per bucket / 10s window).
// It goes through the newGoogleBreaker + defaultSREConfig construction path, covering
// real initialisation.
func realGoogleBreaker() *googleBreaker {
	return newGoogleBreaker(defaultSREConfig())
}

// BenchmarkAcceptEmpty measures the cost of one accept decision with an empty
// window (no history data).
// This is the common path under normal traffic and the one most worth optimising.
func BenchmarkAcceptEmpty(b *testing.B) {
	br := realGoogleBreaker()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = br.accept()
	}
}

// BenchmarkAcceptWithHistory measures the cost of one accept decision when the
// window holds history data.
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

// BenchmarkDo measures the cost of one full request decision through the Breaker
// interface.
func BenchmarkDo(b *testing.B) {
	br := NewBreaker(WithName("bench"))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = br.Do(func() error { return nil })
	}
}

// BenchmarkWindowHistory measures the cost of the rolling-window aggregation
// traversal alone (the core cost of an accept decision).
func BenchmarkWindowHistory(b *testing.B) {
	br := realGoogleBreaker()
	for i := 0; i < 1000; i++ {
		br.markSuccess()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = br.history()
	}
}
