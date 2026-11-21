package pathmatch

import "testing"

func TestMatcherExactPattern(t *testing.T) {
	matcher := New([]string{"/health"})

	if !matcher.Match("/health") {
		t.Fatal("exact rule did not match")
	}
	if matcher.Match("/healthz") {
		t.Fatal("exact rule matched a different path")
	}
}

func TestMatcherPrefixPattern(t *testing.T) {
	matcher := New([]string{"/health*"})

	for _, requestPath := range []string{"/health", "/healthz", "/health/live"} {
		if !matcher.Match(requestPath) {
			t.Errorf("prefix rule did not match %q", requestPath)
		}
	}
	if matcher.Match("/api/health") {
		t.Fatal("prefix rule matched an unrelated path")
	}
}

func TestMatcherTrailingWildcardIsPrefix(t *testing.T) {
	matcher := New([]string{"/metrics/*"})

	for _, requestPath := range []string{"/metrics/cpu", "/metrics/cpu/rate"} {
		if !matcher.Match(requestPath) {
			t.Errorf("prefix rule did not match %q", requestPath)
		}
	}
	if matcher.Match("/other/cpu") {
		t.Fatal("prefix rule matched an unrelated path")
	}
}

func TestMatcherGlobPattern(t *testing.T) {
	matcher := New([]string{"/api/v?/x"})

	if !matcher.Match("/api/v1/x") {
		t.Fatal("glob '?' rule did not match")
	}
	if matcher.Match("/api/v10/x") {
		t.Fatal("glob '?' matched multiple characters")
	}
}

func TestMatcherIgnoresEmptyRules(t *testing.T) {
	matcher := New([]string{"", "  "})

	if !matcher.Empty() {
		t.Fatal("blank rules should be dropped")
	}
	if matcher.Match("/anything") {
		t.Fatal("empty matcher matched a path")
	}
}

func TestNilMatcherMatchesNothing(t *testing.T) {
	var matcher *Matcher

	if matcher.Match("/health") {
		t.Fatal("nil matcher matched a path")
	}
	if !matcher.Empty() {
		t.Fatal("nil matcher should be empty")
	}
}

// TestMatcherCleansDotSegments is a regression test: skipping auth on the raw
// path let "/hooks/../api/apps" match "/hooks*" before ServeMux cleaned it.
func TestMatcherCleansDotSegments(t *testing.T) {
	matcher := New([]string{"/hooks*"})

	if matcher.Match("/hooks/../api/apps") {
		t.Fatal("dot segments must not widen a prefix rule")
	}
	if !matcher.Match("/hooks/./callback") {
		t.Fatal("a cleaned path under the prefix should still match")
	}
	if !matcher.Match("//hooks//callback") {
		t.Fatal("duplicate slashes should be cleaned before matching")
	}
}
