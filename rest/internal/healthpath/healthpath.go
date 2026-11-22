// Package healthpath normalizes and validates the path of the built-in health
// endpoint.
package healthpath

import "strings"

// Default is the path used when the configured health path is empty.
const Default = "/health"

// Normalize trims the configured path and makes it absolute, so a config value
// such as "healthz" still registers "/healthz".
func Normalize(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return Default
	}
	if !strings.HasPrefix(path, "/") {
		return "/" + path
	}
	return path
}

// Valid rejects paths that would make the health route surprising:
//   - "/" (and a trailing slash) makes the ServeMux pattern a subtree that matches
//     every request and shadows the 404 handler;
//   - whitespace, query, fragment and wildcard characters are not a static path.
func Valid(path string) bool {
	if path == "" || path == "/" || strings.HasSuffix(path, "/") {
		return false
	}
	return !strings.ContainsAny(path, " \t\r\n?#{}")
}
