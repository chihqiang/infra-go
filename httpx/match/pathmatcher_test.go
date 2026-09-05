package match

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPathMatcher_Exact(t *testing.T) {
	m := NewPathMatcher([]string{"/health", "/ready"})
	assert.True(t, m.Match("/health"))
	assert.True(t, m.Match("/ready"))
	assert.False(t, m.Match("/healthz"))
	assert.False(t, m.Match("/foo"))
	assert.False(t, m.Match(""))
}

func TestPathMatcher_PrefixWildcard(t *testing.T) {
	// 以 * 结尾：前缀匹配，可跨目录
	m := NewPathMatcher([]string{"/health*"})
	for _, p := range []string{"/health", "/healthz", "/healthy", "/health/live", "/health/a/b"} {
		assert.True(t, m.Match(p), "path %s should match /health*", p)
	}
	for _, p := range []string{"/foo/health", "/api", "/hea"} {
		assert.False(t, m.Match(p), "path %s should NOT match /health*", p)
	}
}

func TestPathMatcher_GlobWildcard(t *testing.T) {
	// glob：* 在段中间时不跨 /，仅匹配单个路径段；支持字符类
	m := NewPathMatcher([]string{"/api/*/x", "/v[0-9]/info"})
	assert.True(t, m.Match("/api/users/x"))
	assert.False(t, m.Match("/api/users/y/x"))
	assert.True(t, m.Match("/v1/info"))
	assert.False(t, m.Match("/v2x/info"))
}

func TestPathMatcher_EmptyPatternIgnored(t *testing.T) {
	m := NewPathMatcher([]string{"", "/health"})
	assert.True(t, m.Match("/health"))
	assert.False(t, m.Match(""))
}

func TestPathMatcher_NoPatterns(t *testing.T) {
	m := NewPathMatcher(nil)
	assert.False(t, m.Match("/anything"))
}
