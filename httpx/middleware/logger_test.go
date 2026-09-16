package middleware

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chihqiang/infra-go/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Covers logger.go: the AccessLogger access log middleware.

// captureLogger redirects the global logger to a temporary file and returns its path, making it
// easy to assert on logged output.
func captureLogger(t *testing.T) string {
	t.Helper()
	logPath := filepath.Join(t.TempDir(), "test.log")
	l := logger.New(logger.Config{Output: []string{logPath}, Caller: false})
	old := logger.GetGlobal()
	logger.SetGlobal(l)
	t.Cleanup(func() {
		logger.SetGlobal(old)
		_ = l.Sync()
	})
	return logPath
}

// readLogLines reads the log file and returns its non-empty lines; when the file does not exist
// (no log was produced) it returns an empty slice.
func readLogLines(t *testing.T, logPath string) []string {
	t.Helper()
	data, err := os.ReadFile(logPath)
	if os.IsNotExist(err) {
		return nil
	}
	require.NoError(t, err)
	var lines []string
	for _, l := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

func TestAccessLogger_WritesLog(t *testing.T) {
	logPath := captureLogger(t)
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }

	rec := perform(NewAccessLogger().Middleware(), ok, httptest.NewRequest(http.MethodGet, "/ok", nil))
	assert.Equal(t, http.StatusOK, rec.Code)

	lines := readLogLines(t, logPath)
	require.NotEmpty(t, lines) // a normal request should write an access log entry
	assert.Contains(t, lines[0], "http request")
}

func TestAccessLogger_CapturesErrorStatus(t *testing.T) {
	logPath := captureLogger(t)
	bad := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusBadRequest) }

	rec := perform(NewAccessLogger().Middleware(), bad, httptest.NewRequest(http.MethodGet, "/bad", nil))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.NotEmpty(t, readLogLines(t, logPath))
}

func TestAccessLogger_SkipExactPaths(t *testing.T) {
	logPath := captureLogger(t)
	mw := NewAccessLogger("/skip", "/skip2").Middleware()

	// Paths hitting skip are not logged
	rec := perform(mw, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) },
		httptest.NewRequest(http.MethodGet, "/skip", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, readLogLines(t, logPath))

	// Other paths are logged normally
	rec2 := perform(mw, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) },
		httptest.NewRequest(http.MethodGet, "/ok", nil))
	assert.Equal(t, http.StatusOK, rec2.Code)
	assert.NotEmpty(t, readLogLines(t, logPath))
}

func TestAccessLogger_SkipPrefixWildcard(t *testing.T) {
	logPath := captureLogger(t)
	mw := NewAccessLogger("/internal/*").Middleware()

	rec := perform(mw, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) },
		httptest.NewRequest(http.MethodGet, "/internal/health", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, readLogLines(t, logPath))
}
