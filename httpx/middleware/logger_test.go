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

// 对应 logger.go：AccessLogger 访问日志中间件。

// captureLogger 将全局 logger 重定向到临时文件并返回路径，便于断言日志写入。
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

// readLogLines 读取日志文件内容并返回非空行；文件不存在（未产生日志）时返回空切片。
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
	require.NotEmpty(t, lines) // 正常请求应写入访问日志
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

	// 命中 skip 的路径不记录日志
	rec := perform(mw, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) },
		httptest.NewRequest(http.MethodGet, "/skip", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, readLogLines(t, logPath))

	// 其它路径正常记录
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
