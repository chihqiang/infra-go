// Package pathmatch matches request paths against skip/ignore rules.
package pathmatch

import (
	"path"
	"strings"
)

// Matcher reports whether a request path hits any of the configured rules.
//
// Supported rule forms:
//   - exact: "/health" matches that path only
//   - prefix: a trailing "*" matches the prefix and everything below it, so
//     "/health*" covers "/healthz" and "/health/live", and "/metrics/*" covers
//     "/metrics/cpu/rate"
//   - glob: "*" and "?" in other positions use path.Match, so "/api/v?/x"
//     matches "/api/v1/x"
//
// Empty rules are ignored and a nil matcher matches nothing.
type Matcher struct {
	patterns []string
}

// New builds a matcher from the given rules.
func New(patterns []string) *Matcher {
	cleaned := make([]string, 0, len(patterns))
	for _, pattern := range patterns {
		if pattern = strings.TrimSpace(pattern); pattern != "" {
			cleaned = append(cleaned, pattern)
		}
	}
	return &Matcher{patterns: cleaned}
}

// Match reports whether requestPath matches any rule.
func (m *Matcher) Match(requestPath string) bool {
	if m == nil {
		return false
	}

	for _, pattern := range m.patterns {
		if matchPattern(pattern, requestPath) {
			return true
		}
	}
	return false
}

// Empty reports whether the matcher has no rules.
func (m *Matcher) Empty() bool {
	return m == nil || len(m.patterns) == 0
}

func matchPattern(pattern, requestPath string) bool {
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(requestPath, strings.TrimSuffix(pattern, "*"))
	}
	if pattern == requestPath {
		return true
	}
	if !strings.ContainsAny(pattern, "*?[") {
		return false
	}

	matched, err := path.Match(pattern, requestPath)
	return err == nil && matched
}
