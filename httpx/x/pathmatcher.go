// Package x collects general-purpose HTTP helpers under httpx that are unrelated to the
// middlewares themselves, for reuse by every module in this library.
//
// It currently contains:
//   - Path matching (PathMatcher): the shared implementation of the skipPaths / ignorePaths
//     rules used by the httpx/middleware middlewares, so they no longer each roll their own;
//   - Client IP resolution (IPChecker / ClientIP): the real client IP behind a reverse proxy.
//
// The name x signals that these scattered helpers are gathered into one set on demand,
// rather than giving every single function its own package; new helpers of the same kind
// belong here.
package x

import (
	"path"
	"strings"
)

// PathMatcher reports whether a request path hits any of a set of ignore/skip rules.
//
// Each pattern supports three forms:
//   - exact match: e.g. "/health", matching that path only;
//   - prefix wildcard: ends with "*", e.g. "/health*", matching paths that start with the
//     prefix (including sub-paths across directories);
//   - glob wildcard: e.g. "/metrics/*" ("*" does not cross directories) or "/api/v?/x",
//     using path.Match semantics.
//
// Note: a rule ending in "*" is first matched as a prefix (which may cross directories), so
// "/metrics/*" also matches "/metrics/a/b". To match a single sub-level only, use a glob
// rule without the trailing "*" ("*" does not cross directories), e.g. "/metrics/?".
//
// Empty-string rules are ignored.
type PathMatcher struct {
	patterns []string
}

// NewPathMatcher builds a path matcher from patterns; it is nil-safe (with no rules it
// matches nothing).
func NewPathMatcher(patterns []string) *PathMatcher {
	return &PathMatcher{patterns: patterns}
}

// Match reports whether reqPath hits any of the rules.
func (m *PathMatcher) Match(reqPath string) bool {
	for _, p := range m.patterns {
		if p == "" {
			continue
		}
		if p == reqPath {
			return true
		}
		// ends with *: prefix match (may cross directories), e.g. /health* matches
		// /healthz, /health/live
		if strings.HasSuffix(p, "*") &&
			strings.HasPrefix(reqPath, strings.TrimSuffix(p, "*")) {
			return true
		}
		// glob match (* does not cross /), e.g. /metrics/* matches /metrics/foo
		if ok, _ := path.Match(p, reqPath); ok {
			return true
		}
	}
	return false
}
