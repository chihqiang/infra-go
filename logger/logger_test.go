package logger

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// jsonLogEntry 解析后的 JSON 日志条目。
type jsonLogEntry struct {
	Level  string `json:"level"`
	Msg    string `json:"msg"`
	Caller string `json:"caller"`
	App    string `json:"app,omitempty"`
	Name   string `json:"name,omitempty"`
	UserID int    `json:"user_id,omitempty"`
}

func parseJSONLog(t *testing.T, line string) jsonLogEntry {
	t.Helper()
	var entry jsonLogEntry
	err := json.Unmarshal([]byte(strings.TrimSpace(line)), &entry)
	require.NoError(t, err, "failed to parse JSON log line: %s", line)
	return entry
}

func readLogFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}

// closeLogger 类型断言后关闭 logger，用于测试中刷新文件输出。
func closeLogger(t *testing.T, l ILogger) {
	t.Helper()
	_ = l.(*Logger).Close()
}

// --- New / Default ---

func TestNew_DefaultConfig(t *testing.T) {
	l := New(Config{})
	require.NotNil(t, l)
	_ = l.Sync()
}

func TestNew_CustomConfig(t *testing.T) {
	l := New(Config{
		Level:    DebugLevel,
		Encoding: ConsoleEncoding,
		Output:   []string{"stdout"},
		AppName:  "test-app",
	})
	require.NotNil(t, l)
}

func TestDefault(t *testing.T) {
	l := Default()
	require.NotNil(t, l)
}

// --- 输出目标 ---

func TestNew_FileOutput(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := tmpDir + "/test.log"

	l := New(Config{
		Level:    InfoLevel,
		Encoding: JSONEncoding,
		Output:   []string{logFile},
	})
	l.Info("file output test", String("key", "value"))
	closeLogger(t, l)

	content := readLogFile(t, logFile)
	assert.Contains(t, content, "file output test")
	assert.Contains(t, content, `"key":"value"`)
}

func TestNew_MultipleOutputs(t *testing.T) {
	tmpDir := t.TempDir()
	logFile1 := tmpDir + "/log1.log"
	logFile2 := tmpDir + "/log2.log"

	l := New(Config{
		Level:  InfoLevel,
		Output: []string{logFile1, logFile2},
	})
	l.Info("multi output test")
	closeLogger(t, l)

	assert.Contains(t, readLogFile(t, logFile1), "multi output test")
	assert.Contains(t, readLogFile(t, logFile2), "multi output test")
}

func TestNew_InvalidOutputFallback(t *testing.T) {
	l := New(Config{
		Output: []string{"/nonexistent_dir/deep/path/log.txt"},
	})
	require.NotNil(t, l)
	l.Info("fallback test")
	_ = l.Sync()
}

func TestNew_DirectoryCreation(t *testing.T) {
	tmpDir := t.TempDir()
	deepPath := tmpDir + "/deep/nested/dir/app.log"

	l := New(Config{Output: []string{deepPath}})
	l.Info("deep path test")
	closeLogger(t, l)

	content := readLogFile(t, deepPath)
	assert.Contains(t, content, "deep path test")
}

// --- 级别过滤 ---

func TestLogger_LevelFiltering(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := tmpDir + "/filter.log"

	l := New(Config{
		Level:    WarnLevel,
		Encoding: JSONEncoding,
		Output:   []string{logFile},
	})

	l.Debug("should not appear")
	l.Info("should not appear")
	l.Warn("should appear")
	l.Error("should appear too")

	closeLogger(t, l)

	content := readLogFile(t, logFile)
	lines := strings.Split(strings.TrimSpace(content), "\n")
	assert.Len(t, lines, 2)
	assert.Contains(t, lines[0], "should appear")
	assert.Contains(t, lines[1], "should appear too")
}

// --- 结构化日志方法 ---

func TestLogger_StructuredMethods(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := tmpDir + "/structured.log"

	l := New(Config{
		Level:    DebugLevel,
		Encoding: JSONEncoding,
		Output:   []string{logFile},
	})

	l.Debug("debug msg", String("k", "v"))
	l.Info("info msg", Int("n", 1))
	l.Warn("warn msg")
	l.Error("error msg")

	closeLogger(t, l)

	content := readLogFile(t, logFile)
	lines := strings.Split(strings.TrimSpace(content), "\n")
	require.Len(t, lines, 4)

	assert.Equal(t, "DEBUG", parseJSONLog(t, lines[0]).Level)
	assert.Equal(t, "debug msg", parseJSONLog(t, lines[0]).Msg)
	assert.Equal(t, "INFO", parseJSONLog(t, lines[1]).Level)
	assert.Equal(t, "info msg", parseJSONLog(t, lines[1]).Msg)
	assert.Equal(t, "WARN", parseJSONLog(t, lines[2]).Level)
	assert.Equal(t, "ERROR", parseJSONLog(t, lines[3]).Level)
}

// --- 格式化日志方法 ---

func TestLogger_FormatMethods(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := tmpDir + "/format.log"

	l := New(Config{
		Level:    DebugLevel,
		Encoding: JSONEncoding,
		Output:   []string{logFile},
	})

	l.Debugf("debug %d", 1)
	l.Infof("info %s", "hello")
	l.Warnf("warn %v", true)
	l.Errorf("error %d", 42)

	closeLogger(t, l)

	content := readLogFile(t, logFile)
	lines := strings.Split(strings.TrimSpace(content), "\n")
	require.Len(t, lines, 4)

	assert.Contains(t, lines[0], "debug 1")
	assert.Contains(t, lines[1], "info hello")
	assert.Contains(t, lines[2], "warn true")
	assert.Contains(t, lines[3], "error 42")
}

// --- 带上下文的结构化日志 ---

func TestLogger_ContextMethods(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := tmpDir + "/ctx.log"

	l := New(Config{
		Level:    InfoLevel,
		Encoding: JSONEncoding,
		Output:   []string{logFile},
	})

	ctx := context.Background()
	l.InfoCtx(ctx, "ctx info")
	l.WarnCtx(ctx, "ctx warn")
	l.ErrorCtx(ctx, "ctx error")

	closeLogger(t, l)

	content := readLogFile(t, logFile)
	assert.Contains(t, content, "ctx info")
	assert.Contains(t, content, "ctx warn")
	assert.Contains(t, content, "ctx error")
}

// --- 带上下文的格式化日志 ---

func TestLogger_FormatContextMethods(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := tmpDir + "/fctx.log"

	l := New(Config{
		Level:    InfoLevel,
		Encoding: JSONEncoding,
		Output:   []string{logFile},
	})

	ctx := context.Background()
	l.InfofCtx(ctx, "fctx info %d", 1)
	l.WarnfCtx(ctx, "fctx warn %s", "msg")

	closeLogger(t, l)

	content := readLogFile(t, logFile)
	assert.Contains(t, content, "fctx info 1")
	assert.Contains(t, content, "fctx warn msg")
}

// --- Console 编码 ---

func TestLogger_ConsoleEncoding(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := tmpDir + "/console.log"

	l := New(Config{
		Level:    InfoLevel,
		Encoding: ConsoleEncoding,
		Output:   []string{logFile},
	})
	l.Info("console mode test")

	closeLogger(t, l)

	content := readLogFile(t, logFile)
	assert.Contains(t, content, "INFO")
	assert.Contains(t, content, "console mode test")
}

// --- Caller ---

func TestLogger_Caller(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := tmpDir + "/caller.log"

	l := New(Config{
		Level:    InfoLevel,
		Encoding: JSONEncoding,
		Output:   []string{logFile},
		Caller:   true,
	})
	l.Info("with caller")

	closeLogger(t, l)

	entry := parseJSONLog(t, readLogFile(t, logFile))
	assert.NotEmpty(t, entry.Caller)
	assert.Contains(t, entry.Caller, "logger_test.go")
}

// --- Stacktrace ---

func TestLogger_StackTrace(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := tmpDir + "/stacktrace.log"

	l := New(Config{
		Level:      InfoLevel,
		Encoding:   JSONEncoding,
		Output:     []string{logFile},
		Stacktrace: true,
	})
	l.Error("error with stacktrace")

	closeLogger(t, l)

	content := readLogFile(t, logFile)
	assert.Contains(t, content, "stacktrace")
}

// --- Sync ---

func TestLogger_Sync(t *testing.T) {
	l := New(Config{Output: []string{"stdout"}})
	l.Info("sync test")
	_ = l.Sync()
}

// --- 全局 Logger ---

func TestGlobal_Default(t *testing.T) {
	g := GetGlobal()
	require.NotNil(t, g)
	assert.NotPanics(t, func() {
		g.Info("global logger test")
	})
}

func TestGlobal_SetGlobal(t *testing.T) {
	old := GetGlobal()
	defer SetGlobal(old)

	custom := New(Config{
		Level:   DebugLevel,
		AppName: "global-test",
	})
	SetGlobal(custom)

	g := GetGlobal()
	require.NotNil(t, g)
	assert.NotPanics(t, func() {
		g.Info("set global test")
	})
}

func TestGlobal_ReplaceGlobal(t *testing.T) {
	old := GetGlobal()
	defer SetGlobal(old)

	replaced := ReplaceGlobal(Config{
		Level:   ErrorLevel,
		AppName: "replaced",
	})
	require.NotNil(t, replaced)

	g := GetGlobal()
	require.NotNil(t, g)
}

func TestGlobal_StructuredMethods(t *testing.T) {
	assert.NotPanics(t, func() {
		Debug("global debug")
		Info("global info")
		Warn("global warn")
		Error("global error")
	})
}

func TestGlobal_FormatMethods(t *testing.T) {
	assert.NotPanics(t, func() {
		Debugf("debug %s", "x")
		Infof("info %s", "x")
		Warnf("warn %s", "x")
		Errorf("error %s", "x")
	})
}

func TestGlobal_ContextMethods(t *testing.T) {
	ctx := context.Background()
	assert.NotPanics(t, func() {
		DebugfCtx(ctx, "ctx debug %s", "x")
		InfofCtx(ctx, "ctx info %s", "x")
		WarnfCtx(ctx, "ctx warn %s", "x")
		ErrorfCtx(ctx, "ctx error %s", "x")
	})
}

func TestGlobal_FormatContextMethods(t *testing.T) {
	ctx := context.Background()
	assert.NotPanics(t, func() {
		DebugfCtx(ctx, "fctx debug %s", "x")
		InfofCtx(ctx, "fctx info %s", "x")
		WarnfCtx(ctx, "fctx warn %s", "x")
		ErrorfCtx(ctx, "fctx error %s", "x")
	})
}

func TestGlobal_Sync(t *testing.T) {
	_ = Sync()
}

// --- 字段构造函数 ---

func TestFieldConstructors(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := tmpDir + "/fields.log"

	l := New(Config{
		Level:    DebugLevel,
		Encoding: JSONEncoding,
		Output:   []string{logFile},
	})

	l.Info("field test",
		String("str", "hello"),
		Int("num", 42),
		Int64("big", 9223372036854775807),
		Float64("pi", 3.14),
		Bool("flag", true),
		Duration("elapsed", 5*time.Second),
		Any("data", map[string]int{"a": 1}),
	)
	closeLogger(t, l)

	content := readLogFile(t, logFile)
	assert.Contains(t, content, `"str":"hello"`)
	assert.Contains(t, content, `"num":42`)
	assert.Contains(t, content, `"big":9223372036854775807`)
	assert.Contains(t, content, `"pi":3.14`)
	assert.Contains(t, content, `"flag":true`)
	assert.Contains(t, content, `"elapsed":5000`)
	assert.Contains(t, content, `"data":{"a":1}`)
}

func TestFieldErr(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := tmpDir + "/err.log"

	l := New(Config{
		Level:    ErrorLevel,
		Encoding: JSONEncoding,
		Output:   []string{logFile},
	})

	err := assert.AnError
	l.Error("operation failed", Err(err))
	closeLogger(t, l)

	content := readLogFile(t, logFile)
	assert.Contains(t, content, `"error"`)
	assert.Contains(t, content, assert.AnError.Error())
}

// --- 默认值填充 ---

func TestFillDefault_AllDefaults(t *testing.T) {
	c := fillDefault(Config{})

	assert.Equal(t, InfoLevel, c.Level)
	assert.Equal(t, JSONEncoding, c.Encoding)
	assert.Equal(t, []string{"stdout"}, c.Output)
	assert.Equal(t, "stderr", c.ErrorOutput)
	assert.True(t, c.Caller)
	assert.False(t, c.Stacktrace)
	assert.Equal(t, "2006-01-02T15:04:05.000Z07:00", c.TimeLayout)
	assert.Equal(t, "", c.AppName)
	assert.Equal(t, 100, c.Rotation.MaxSize)
	assert.Equal(t, 7, c.Rotation.MaxBackups)
	assert.Equal(t, 30, c.Rotation.MaxAge)
	assert.False(t, c.Rotation.Compress)
	assert.True(t, c.Rotation.LocalTime)
}

func TestFillDefault_UserOverrides(t *testing.T) {
	c := fillDefault(Config{
		Level:    DebugLevel,
		Encoding: ConsoleEncoding,
		Output:   []string{"/var/log/app.log"},
		AppName:  "myapp",
		Rotation: RotationConfig{
			MaxSize:  50,
			Compress: true,
		},
	})

	assert.Equal(t, DebugLevel, c.Level)
	assert.Equal(t, ConsoleEncoding, c.Encoding)
	assert.Equal(t, []string{"/var/log/app.log"}, c.Output)
	assert.Equal(t, "myapp", c.AppName)
	assert.Equal(t, 50, c.Rotation.MaxSize)
	assert.True(t, c.Rotation.Compress)
	assert.Equal(t, "stderr", c.ErrorOutput)
	assert.True(t, c.Caller)
	assert.Equal(t, 7, c.Rotation.MaxBackups)
	assert.Equal(t, 30, c.Rotation.MaxAge)
	assert.True(t, c.Rotation.LocalTime)
}

// --- 日志轮转 ---

func TestRotation_FileSize(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := tmpDir + "/rotate.log"

	l := New(Config{
		Level:    InfoLevel,
		Encoding: JSONEncoding,
		Output:   []string{logFile},
		Rotation: RotationConfig{
			MaxSize:    1,
			MaxBackups: 3,
			MaxAge:     0,
		},
	})

	for i := 0; i < 20000; i++ {
		l.Info("rotation test message", Int("seq", i))
	}
	closeLogger(t, l)

	content := readLogFile(t, logFile)
	assert.NotEmpty(t, content)

	matches, err := filepath.Glob(tmpDir + "/rotate-*.log")
	require.NoError(t, err)
	assert.NotEmpty(t, matches, "should have backup log files after rotation")
}

// --- 接口合规性 ---

func TestILogger_Compliance(t *testing.T) {
	var l ILogger = New(Config{})
	assert.NotNil(t, l)
}

// --- 上下文字段提取器的注册与注销 ---

// countFields 统计 extractContextFields 产出中指定 key 的出现次数。
func countFields(t *testing.T, key string) int {
	t.Helper()
	n := 0
	for _, f := range extractContextFields(context.Background()) {
		if f.Key == key {
			n++
		}
	}
	return n
}

func TestRegisterContextExtractor_Unregister(t *testing.T) {
	const key = "zz_test_unregister"
	unregister := RegisterContextExtractor(func(context.Context) []Field {
		return []Field{String(key, "v")}
	})
	require.Equal(t, 1, countFields(t, key))

	unregister()
	assert.Equal(t, 0, countFields(t, key), "注销后不应再提取到该字段")

	// 幂等：重复调用不应 panic，也不应影响其它提取器
	assert.NotPanics(t, unregister)
	assert.NotPanics(t, unregister)
}

func TestRegisterContextExtractor_UnregisterIsolated(t *testing.T) {
	// 注销其中一个不应影响另一个（验证按条目标记而非按下标删除）
	const keyA, keyB = "zz_test_iso_a", "zz_test_iso_b"
	unregisterA := RegisterContextExtractor(func(context.Context) []Field {
		return []Field{String(keyA, "v")}
	})
	unregisterB := RegisterContextExtractor(func(context.Context) []Field {
		return []Field{String(keyB, "v")}
	})
	t.Cleanup(func() { unregisterA(); unregisterB() })

	unregisterA()
	assert.Equal(t, 0, countFields(t, keyA))
	assert.Equal(t, 1, countFields(t, keyB), "注销 A 不应连带移除 B")
}

func TestRegisterContextExtractor_Nil(t *testing.T) {
	// nil 提取器不注册，返回的注销函数为空操作
	unregister := RegisterContextExtractor(nil)
	require.NotNil(t, unregister)
	assert.NotPanics(t, unregister)
}

// TestRegisterContextExtractor_DuplicateProducesDuplicateFields 锁定注册无法去重的
// 事实：Go 中函数值不可比较，所以同一提取器注册两次会产生两份字段。
// 这正是需要注销函数的原因。
func TestRegisterContextExtractor_DuplicateProducesDuplicateFields(t *testing.T) {
	const key = "zz_test_dup"
	extractor := func(context.Context) []Field { return []Field{String(key, "v")} }

	unregister1 := RegisterContextExtractor(extractor)
	unregister2 := RegisterContextExtractor(extractor)
	t.Cleanup(func() { unregister1(); unregister2() })

	assert.Equal(t, 2, countFields(t, key), "重复注册会产生重复字段，需用返回的注销函数撤销")

	unregister1()
	assert.Equal(t, 1, countFields(t, key))
}

// TestRegisterContextExtractor_Concurrent 验证注册/注销与提取并发时无数据竞争
// （注销使用原子标记 + 整体替换切片头，不原地修改正在被遍历的底层数组）。
func TestRegisterContextExtractor_Concurrent(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			unregister := RegisterContextExtractor(func(context.Context) []Field {
				return []Field{String("zz_test_concurrent", "v")}
			})
			_ = extractContextFields(context.Background())
			unregister()
		}()
	}
	wg.Wait()
}
