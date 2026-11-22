package logger

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type jsonLogEntry struct {
	Level      string `json:"level"`
	Time       string `json:"time"`
	Msg        string `json:"msg"`
	Caller     string `json:"caller"`
	App        string `json:"app,omitempty"`
	Stacktrace string `json:"stacktrace,omitempty"`
	Key        string `json:"key,omitempty"`
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

func closeLogger(t *testing.T, l ILogger) {
	t.Helper()
	_ = l.(*Logger).Close()
}

func fileLogger(t *testing.T, cfg Config) (*Logger, string) {
	t.Helper()
	dir := t.TempDir()
	cfg.Mode = "file"
	cfg.Path = dir
	l := New(cfg).(*Logger)
	t.Cleanup(func() { _ = l.Close() })
	return l, dir
}

func TestNew_DefaultConfig(t *testing.T) {
	l := New(Config{})
	require.NotNil(t, l)
	_ = l.Sync()
	_ = l.(*Logger).Close()
}

func TestNew_CustomConfig(t *testing.T) {
	l := New(Config{
		ServiceName: "test-app",
		Mode:        "file",
		Encoding:    "json",
		Path:        t.TempDir(),
		Level:       "debug",
	})
	require.NotNil(t, l)
	_ = l.(*Logger).Close()
}

func TestDefault(t *testing.T) {
	l := Default()
	require.NotNil(t, l)
	_ = l.(*Logger).Close()
}

// --- Output targets ---

func TestNew_FileOutput(t *testing.T) {
	l, dir := fileLogger(t, Config{Level: "info", Encoding: "json"})
	l.Info("file output test", String("key", "value"))
	_ = l.Close()

	content := readLogFile(t, filepath.Join(dir, "access.log"))
	assert.Contains(t, content, "file output test")
	assert.Contains(t, content, `"key":"value"`)
}

func TestNew_ErrorFileOutput(t *testing.T) {
	l, dir := fileLogger(t, Config{Level: "info", Encoding: "json"})
	l.Error("error file test")
	_ = l.Close()

	content := readLogFile(t, filepath.Join(dir, "error.log"))
	assert.Contains(t, content, "error file test")
}

func TestNew_DirectoryCreation(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "deep", "nested", "dir")
	l := New(Config{Mode: "file", Path: dir}).(*Logger)
	l.Info("deep path test")
	_ = l.Close()

	content := readLogFile(t, filepath.Join(dir, "access.log"))
	assert.Contains(t, content, "deep path test")
}

func TestNew_InvalidPathFallback(t *testing.T) {
	l := New(Config{Mode: "file", Path: "/dev/null/not-a-directory"})
	require.NotNil(t, l)
	l.Info("fallback test")
	_ = l.Sync()
	_ = l.(*Logger).Close()
}

// --- Level filtering ---

func TestLogger_LevelFiltering(t *testing.T) {
	l, dir := fileLogger(t, Config{Level: "error", Encoding: "json"})
	l.Debug("should not appear")
	l.Info("should not appear")
	l.Warn("should not appear")
	l.Error("should appear")

	_ = l.Close()

	content := readLogFile(t, filepath.Join(dir, "error.log"))
	lines := strings.Split(strings.TrimSpace(content), "\n")
	require.Len(t, lines, 1)
	assert.Contains(t, lines[0], "should appear")
}

func TestLogger_SevereLevelFiltering(t *testing.T) {
	l, dir := fileLogger(t, Config{Level: "severe", Encoding: "json"})
	assert.False(t, l.enabled(ErrorLevel))
	assert.True(t, l.enabled(FatalLevel))

	l.Debug("should not appear")
	l.Info("should not appear")
	l.Warn("should not appear")
	l.Error("should not appear")

	_ = l.Close()

	assert.Empty(t, readLogFile(t, filepath.Join(dir, "access.log")))
	assert.Empty(t, readLogFile(t, filepath.Join(dir, "error.log")))
}

func TestParseLevel(t *testing.T) {
	tests := map[string]Level{
		"debug":   DebugLevel,
		"info":    InfoLevel,
		"warn":    WarnLevel,
		"warning": WarnLevel,
		"error":   ErrorLevel,
		"severe":  FatalLevel,
		"dpanic":  DPanicLevel,
		"panic":   PanicLevel,
		"fatal":   FatalLevel,
		" INFO ":  InfoLevel,
		"unknown": InfoLevel,
	}

	for input, want := range tests {
		assert.Equal(t, want, parseLevel(input), "parseLevel(%q)", input)
	}
}

func TestLevelString(t *testing.T) {
	tests := map[Level]string{
		DebugLevel:  "DEBUG",
		InfoLevel:   "INFO",
		WarnLevel:   "WARN",
		ErrorLevel:  "ERROR",
		DPanicLevel: "DPANIC",
		PanicLevel:  "PANIC",
		FatalLevel:  "FATAL",
		Level(99):   "UNKNOWN",
	}

	for level, want := range tests {
		assert.Equal(t, want, level.String(), "Level(%d).String()", level)
	}
}

// --- Structured logging methods ---

func TestLogger_StructuredMethods(t *testing.T) {
	l, dir := fileLogger(t, Config{Level: "debug", Encoding: "json"})
	l.Debug("debug msg", String("k", "v"))
	l.Info("info msg", Int("n", 1))
	l.Warn("warn msg")
	l.Error("error msg")
	_ = l.Close()

	content := readLogFile(t, filepath.Join(dir, "access.log"))
	lines := strings.Split(strings.TrimSpace(content), "\n")
	require.Len(t, lines, 3)
	assert.Equal(t, "DEBUG", parseJSONLog(t, lines[0]).Level)
	assert.Equal(t, "INFO", parseJSONLog(t, lines[1]).Level)
	assert.Equal(t, "WARN", parseJSONLog(t, lines[2]).Level)

	errorContent := readLogFile(t, filepath.Join(dir, "error.log"))
	assert.Contains(t, errorContent, "error msg")
}

// --- Formatted logging methods ---

func TestLogger_FormatMethods(t *testing.T) {
	l, dir := fileLogger(t, Config{Level: "debug", Encoding: "json"})
	l.Debugf("debug %d", 1)
	l.Infof("info %s", "hello")
	l.Warnf("warn %v", true)
	l.Errorf("error %d", 42)
	_ = l.Close()

	content := readLogFile(t, filepath.Join(dir, "access.log"))
	lines := strings.Split(strings.TrimSpace(content), "\n")
	require.Len(t, lines, 3)
	assert.Contains(t, lines[0], "debug 1")
	assert.Contains(t, lines[1], "info hello")
	assert.Contains(t, lines[2], "warn true")
	assert.Contains(t, readLogFile(t, filepath.Join(dir, "error.log")), "error 42")
}

// --- Structured logging with context ---

func TestLogger_ContextMethods(t *testing.T) {
	l, dir := fileLogger(t, Config{Level: "info", Encoding: "json"})
	unregister := RegisterContextExtractor(func(context.Context) []Field {
		return []Field{String("request_id", "req-1")}
	})
	t.Cleanup(unregister)
	ctx := context.Background()
	l.InfoCtx(ctx, "ctx info")
	l.ErrorfCtx(ctx, "ctx error %d", 1)
	_ = l.Close()

	content := readLogFile(t, filepath.Join(dir, "access.log"))
	assert.Contains(t, content, `"request_id":"req-1"`)
	assert.Contains(t, content, "ctx info")
	assert.Contains(t, readLogFile(t, filepath.Join(dir, "error.log")), "ctx error 1")
}

func TestLogger_ContextMethodsBelowLevel(t *testing.T) {
	l, dir := fileLogger(t, Config{Level: "fatal", Encoding: "json"})
	ctx := context.Background()
	l.DebugCtx(ctx, "debug ctx")
	l.InfoCtx(ctx, "info ctx")
	l.WarnCtx(ctx, "warn ctx")
	l.ErrorCtx(ctx, "error ctx")
	l.DebugfCtx(ctx, "debug %s", "ctx")
	l.InfofCtx(ctx, "info %s", "ctx")
	l.WarnfCtx(ctx, "warn %s", "ctx")
	l.ErrorfCtx(ctx, "error %s", "ctx")
	_ = l.Close()

	assert.Empty(t, readLogFile(t, filepath.Join(dir, "access.log")))
	assert.Empty(t, readLogFile(t, filepath.Join(dir, "error.log")))
}

func TestLogger_Panic(t *testing.T) {
	l, dir := fileLogger(t, Config{Level: "info", Encoding: "json"})
	assert.PanicsWithValue(t, "boom", func() { l.Panic("boom") })
	assert.PanicsWithValue(t, "boom 1", func() { l.Panicf("boom %d", 1) })
	assert.PanicsWithValue(t, "boom 2", func() {
		l.PanicfCtx(context.Background(), "boom %d", 2)
	})
	_ = l.Close()

	lines := strings.Split(strings.TrimSpace(readLogFile(t, filepath.Join(dir, "error.log"))), "\n")
	require.Len(t, lines, 3)
	assert.Equal(t, "PANIC", parseJSONLog(t, lines[0]).Level)
	assert.Equal(t, "boom", parseJSONLog(t, lines[0]).Msg)
	assert.Equal(t, "boom 1", parseJSONLog(t, lines[1]).Msg)
	assert.Equal(t, "boom 2", parseJSONLog(t, lines[2]).Msg)
}

func TestLogger_FatalExits(t *testing.T) {
	if mode := os.Getenv("LOGGER_FATAL_MODE"); mode != "" {
		l := New(Config{
			Mode:     "file",
			Path:     os.Getenv("LOGGER_FATAL_DIR"),
			Level:    "info",
			Encoding: "json",
		})
		SetGlobal(l)
		ctx := context.Background()
		switch mode {
		case "Fatal":
			l.Fatal("fatal message")
		case "Fatalf":
			l.Fatalf("fatal %d", 1)
		case "FatalCtx":
			l.FatalCtx(ctx, "fatal message")
		case "FatalfCtx":
			l.FatalfCtx(ctx, "fatal %d", 1)
		case "global Fatal":
			Fatal("fatal message")
		case "global Fatalf":
			Fatalf("fatal %d", 1)
		case "global FatalCtx":
			FatalCtx(ctx, "fatal message")
		case "global FatalfCtx":
			FatalfCtx(ctx, "fatal %d", 1)
		}
		// Fatal must terminate the process with exit code 1; any other code means it
		// returned and the parent test fails.
		os.Exit(3)
	}

	for _, mode := range []string{
		"Fatal", "Fatalf", "FatalCtx", "FatalfCtx",
		"global Fatal", "global Fatalf", "global FatalCtx", "global FatalfCtx",
	} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			cmd := exec.Command(os.Args[0], "-test.run=^TestLogger_FatalExits$")
			cmd.Env = append(os.Environ(),
				"LOGGER_FATAL_MODE="+mode,
				"LOGGER_FATAL_DIR="+dir,
			)
			err := cmd.Run()
			var exitErr *exec.ExitError
			require.ErrorAs(t, err, &exitErr)
			assert.Equal(t, 1, exitErr.ExitCode())

			content, readErr := os.ReadFile(filepath.Join(dir, "severe.log"))
			require.NoError(t, readErr)
			assert.Contains(t, string(content), "FATAL")
		})
	}
}

// --- Console / plain encoding ---

func TestLogger_PlainEncoding(t *testing.T) {
	l, dir := fileLogger(t, Config{Level: "info", Encoding: "plain"})
	l.Info("plain mode test", String("key", "value"))
	_ = l.Close()

	content := readLogFile(t, filepath.Join(dir, "access.log"))
	assert.Contains(t, content, "INFO")
	assert.Contains(t, content, "plain mode test")
	assert.Contains(t, content, "key=value")
}

func TestLogger_PlainEncodingFieldTypes(t *testing.T) {
	l, dir := fileLogger(t, Config{Level: "error", Encoding: "plain", ServiceName: "svc"})
	ts := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	l.Error("plain fields",
		String("str", "hello"),
		Int("num", -3),
		Float64("float", 1.5),
		Bool("flag", false),
		Duration("elapsed", 1500*time.Millisecond),
		Time("at", ts),
		Err(assert.AnError),
		Any("list", []int{1, 2}),
	)
	_ = l.Close()

	content := readLogFile(t, filepath.Join(dir, "error.log"))
	assert.Contains(t, content, " app=svc ")
	assert.Contains(t, content, "str=hello")
	assert.Contains(t, content, "num=-3")
	assert.Contains(t, content, "float=1.5")
	assert.Contains(t, content, "flag=false")
	assert.Contains(t, content, "elapsed=1.5s")
	assert.Contains(t, content, "at="+ts.Format(defaultTimeFormat))
	assert.Contains(t, content, assert.AnError.Error())
	assert.Contains(t, content, "list=[1 2]")
	assert.Contains(t, content, "stacktrace=")
}

// --- Caller ---

func TestLogger_Caller(t *testing.T) {
	l, dir := fileLogger(t, Config{Level: "info", Encoding: "json"})
	l.Info("with caller")
	_ = l.Close()

	entry := parseJSONLog(t, readLogFile(t, filepath.Join(dir, "access.log")))
	assert.NotEmpty(t, entry.Caller)
	assert.Contains(t, entry.Caller, "logger_test.go")
}

func TestGlobal_Caller(t *testing.T) {
	prev := GetGlobal()
	l, dir := fileLogger(t, Config{Level: "info", Encoding: "json"})
	SetGlobal(l)
	t.Cleanup(func() { SetGlobal(prev) })

	Info("global caller")
	ErrorCtx(context.Background(), "global ctx caller")
	_ = l.Close()

	entry := parseJSONLog(t, readLogFile(t, filepath.Join(dir, "access.log")))
	assert.Contains(t, entry.Caller, "logger_test.go")
	assert.NotContains(t, entry.Caller, "log.go")

	errorEntry := parseJSONLog(t, readLogFile(t, filepath.Join(dir, "error.log")))
	assert.Contains(t, errorEntry.Caller, "logger_test.go")
	assert.NotContains(t, errorEntry.Caller, "log.go")
}

// --- Stacktrace ---

func TestLogger_StackTrace(t *testing.T) {
	l, dir := fileLogger(t, Config{Level: "info", Encoding: "json"})
	l.Error("error with stacktrace")
	_ = l.Close()

	entry := parseJSONLog(t, readLogFile(t, filepath.Join(dir, "error.log")))
	assert.NotEmpty(t, entry.Stacktrace)
}

func TestLogger_StackCooldown(t *testing.T) {
	l, dir := fileLogger(t, Config{
		Level:               "info",
		Encoding:            "json",
		StackCooldownMillis: 10_000,
	})
	l.Error("first error")
	l.Error("second error")
	_ = l.Close()

	lines := strings.Split(strings.TrimSpace(readLogFile(t, filepath.Join(dir, "error.log"))), "\n")
	require.Len(t, lines, 2)
	assert.NotEmpty(t, parseJSONLog(t, lines[0]).Stacktrace)
	assert.Empty(t, parseJSONLog(t, lines[1]).Stacktrace)
}

// --- Sync / Close ---

func TestLogger_Sync(t *testing.T) {
	l, _ := fileLogger(t, Config{})
	l.Info("sync test")
	require.NoError(t, l.Sync())
}

func TestLogger_CloseIsIdempotent(t *testing.T) {
	l, _ := fileLogger(t, Config{})
	require.NoError(t, l.Close())
	require.NoError(t, l.Close())
}

// --- Global Logger ---

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

	custom := New(Config{Level: "debug", ServiceName: "global-test"})
	SetGlobal(custom)

	g := GetGlobal()
	require.NotNil(t, g)
	assert.NotPanics(t, func() {
		g.Info("set global test")
	})
	_ = custom.(*Logger).Close()
}

func TestGlobal_ReplaceGlobal(t *testing.T) {
	old := GetGlobal()
	defer SetGlobal(old)

	replaced := ReplaceGlobal(Config{Level: "error", ServiceName: "replaced"})
	require.NotNil(t, replaced)

	g := GetGlobal()
	require.NotNil(t, g)
	_ = g.(*Logger).Close()
	_ = replaced // the old logger is restored by the deferred SetGlobal
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

func TestGlobal_StructuredWrappers(t *testing.T) {
	prev := GetGlobal()
	l, dir := fileLogger(t, Config{Level: "debug", Encoding: "json"})
	SetGlobal(l)
	t.Cleanup(func() { SetGlobal(prev) })

	ctx := context.Background()
	DebugCtx(ctx, "debug ctx")
	DebugfCtx(ctx, "debug %s ctx", "formatted")
	InfoCtx(ctx, "info ctx")
	WarnCtx(ctx, "warn ctx")
	ErrorCtx(ctx, "error ctx")
	assert.PanicsWithValue(t, "global panic", func() { Panic("global panic") })
	assert.PanicsWithValue(t, "global panic 1", func() { Panicf("global panic %d", 1) })
	assert.PanicsWithValue(t, "ctx panic", func() { PanicCtx(ctx, "ctx panic") })
	assert.PanicsWithValue(t, "ctx panic 1", func() { PanicfCtx(ctx, "ctx panic %d", 1) })

	// Buffered sinks reach the file on Sync (or Close); panic entries flush
	// themselves, the rest still sit in the buffer.
	require.NoError(t, l.Sync())

	access := readLogFile(t, filepath.Join(dir, "access.log"))
	assert.Contains(t, access, "debug ctx")
	assert.Contains(t, access, "debug formatted ctx")
	assert.Contains(t, access, "info ctx")
	assert.Contains(t, access, "warn ctx")
	assert.NotContains(t, access, "error ctx")

	errorContent := readLogFile(t, filepath.Join(dir, "error.log"))
	assert.Contains(t, errorContent, "error ctx")
	assert.Contains(t, errorContent, "PANIC")
}

func TestGlobal_Sync(t *testing.T) {
	_ = Sync()
}

// --- Field constructors ---

func TestFieldConstructors(t *testing.T) {
	l, dir := fileLogger(t, Config{Level: "debug", Encoding: "json"})
	l.Info("field test",
		String("str", "hello"),
		Int("num", 42),
		Int64("big", 9223372036854775807),
		Float64("pi", 3.14),
		Bool("flag", true),
		Duration("elapsed", 5*time.Second),
		Any("data", map[string]int{"a": 1}),
	)
	_ = l.Close()

	content := readLogFile(t, filepath.Join(dir, "access.log"))
	assert.Contains(t, content, `"str":"hello"`)
	assert.Contains(t, content, `"num":42`)
	assert.Contains(t, content, `"big":9223372036854775807`)
	assert.Contains(t, content, `"pi":3.14`)
	assert.Contains(t, content, `"flag":true`)
	assert.Contains(t, content, `"elapsed":5000`)
	assert.Contains(t, content, `"data":{"a":1}`)
}

func TestFieldErr(t *testing.T) {
	l, dir := fileLogger(t, Config{Level: "error", Encoding: "json"})
	l.Error("operation failed", Err(assert.AnError))
	_ = l.Close()

	content := readLogFile(t, filepath.Join(dir, "error.log"))
	assert.Contains(t, content, `"error"`)
	assert.Contains(t, content, assert.AnError.Error())
}

func TestFieldTimeAndNilErr(t *testing.T) {
	l, dir := fileLogger(t, Config{Level: "info", Encoding: "json"})
	ts := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	l.Info("edge fields", Time("at", ts), Err(nil))
	_ = l.Close()

	content := readLogFile(t, filepath.Join(dir, "access.log"))
	assert.Contains(t, content, `"at":"`+ts.Format(defaultTimeFormat)+`"`)
	assert.Contains(t, content, `"error":null`)
}

// --- Default value filling ---

func TestFillDefault_AllDefaults(t *testing.T) {
	c := Config{}.withDefaults()

	assert.Equal(t, "console", c.Mode)
	assert.Equal(t, "json", c.Encoding)
	assert.Equal(t, "", c.ServiceName)
	assert.Equal(t, "logs", c.Path)
	assert.Equal(t, "info", c.Level)
	assert.Equal(t, defaultTimeFormat, c.TimeFormat)
	assert.True(t, c.Stat)
	assert.Equal(t, 100, c.StackCooldownMillis)
	assert.Equal(t, 0, c.MaxBackups)
	assert.Equal(t, 0, c.MaxSize)
	assert.Equal(t, 0, c.KeepDays)
	assert.Equal(t, "daily", c.Rotation)
	assert.False(t, c.Compress)
}

func TestFillDefault_UserOverrides(t *testing.T) {
	c := Config{
		ServiceName:         "svc",
		Mode:                "file",
		Encoding:            "plain",
		TimeFormat:          time.RFC3339,
		Path:                "/var/log/app",
		Level:               "debug",
		MaxContentLength:    128,
		Compress:            true,
		Stat:                false,
		KeepDays:            3,
		StackCooldownMillis: 5,
		MaxBackups:          2,
		MaxSize:             10,
		Rotation:            "size",
	}.withDefaults()

	assert.Equal(t, "svc", c.ServiceName)
	assert.Equal(t, "file", c.Mode)
	assert.Equal(t, "plain", c.Encoding)
	assert.Equal(t, "/var/log/app", c.Path)
	assert.Equal(t, "debug", c.Level)
	assert.Equal(t, uint32(128), c.MaxContentLength)
	assert.True(t, c.Compress)
	assert.Equal(t, 3, c.KeepDays)
	assert.Equal(t, 5, c.StackCooldownMillis)
	assert.Equal(t, 2, c.MaxBackups)
	assert.Equal(t, 10, c.MaxSize)
	assert.Equal(t, "size", c.Rotation)
}

// --- Log rotation ---

func TestRotation_FileSize(t *testing.T) {
	l, dir := fileLogger(t, Config{
		Level:      "info",
		Encoding:   "json",
		Rotation:   "size",
		MaxSize:    1,
		MaxBackups: 3,
	})
	for i := 0; i < 20000; i++ {
		l.Info("rotation test message", Int("seq", i))
	}
	_ = l.Close()

	content := readLogFile(t, filepath.Join(dir, "access.log"))
	assert.NotEmpty(t, content)

	matches, err := filepath.Glob(filepath.Join(dir, "access-*.log*"))
	require.NoError(t, err)
	assert.NotEmpty(t, matches, "should have backup log files after rotation")
}

func TestRotation_Daily(t *testing.T) {
	dir := t.TempDir()
	writer, err := newRotatingWriter(filepath.Join(dir, "access.log"), Config{
		Rotation:   "daily",
		MaxBackups: 3,
	})
	require.NoError(t, err)
	writer.currentDay = "2000-01-01"
	_, err = writer.Write([]byte("first\n"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	matches, err := filepath.Glob(filepath.Join(dir, "access-*.log"))
	require.NoError(t, err)
	assert.NotEmpty(t, matches)
}

func TestRotation_Compress(t *testing.T) {
	dir := t.TempDir()
	writer, err := newRotatingWriter(filepath.Join(dir, "access.log"), Config{
		Rotation:   "size",
		MaxBackups: 5,
		Compress:   true,
	})
	require.NoError(t, err)

	_, err = writer.Write([]byte("first entry\n"))
	require.NoError(t, err)
	writer.maxSize = 1
	_, err = writer.Write([]byte("second entry\n"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	matches, err := filepath.Glob(filepath.Join(dir, "access-*.log.gz"))
	require.NoError(t, err)
	require.Len(t, matches, 1)

	compressed, err := os.Open(matches[0])
	require.NoError(t, err)
	defer func() { _ = compressed.Close() }()
	reader, err := gzip.NewReader(compressed)
	require.NoError(t, err)
	defer func() { _ = reader.Close() }()
	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Contains(t, string(data), "first entry")

	active := readLogFile(t, filepath.Join(dir, "access.log"))
	assert.Contains(t, active, "second entry")
	assert.NotContains(t, active, "first entry")
}

func TestRotation_MaxBackups(t *testing.T) {
	dir := t.TempDir()
	writer, err := newRotatingWriter(filepath.Join(dir, "access.log"), Config{
		Rotation:   "size",
		MaxBackups: 2,
	})
	require.NoError(t, err)

	for i := 0; i < 5; i++ {
		_, err = writer.Write([]byte("entry\n"))
		require.NoError(t, err)
		writer.maxSize = 1
	}
	require.NoError(t, writer.Close())

	matches, err := filepath.Glob(filepath.Join(dir, "access-*.log"))
	require.NoError(t, err)
	assert.Len(t, matches, 2)
}

func TestRotation_KeepDays(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "access-2000-01-01T00-00-00.000000000.log")
	require.NoError(t, os.WriteFile(old, []byte("old\n"), 0o600))
	past := time.Now().Add(-48 * time.Hour)
	require.NoError(t, os.Chtimes(old, past, past))

	writer, err := newRotatingWriter(filepath.Join(dir, "access.log"), Config{
		Rotation: "size",
		KeepDays: 1,
	})
	require.NoError(t, err)
	_, err = writer.Write([]byte("current\n"))
	require.NoError(t, err)
	writer.maxSize = 1
	_, err = writer.Write([]byte("next\n"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	_, statErr := os.Stat(old)
	assert.True(t, os.IsNotExist(statErr), "expired backup should be removed")
}

func TestRotatingWriter_OperationsAfterClose(t *testing.T) {
	dir := t.TempDir()
	writer, err := newRotatingWriter(filepath.Join(dir, "access.log"), Config{})
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	require.NoError(t, writer.Close())
	require.NoError(t, writer.Sync())

	_, err = writer.Write([]byte("too late\n"))
	assert.Error(t, err)
}

// --- Interface compliance ---

func TestILogger_Compliance(t *testing.T) {
	var l ILogger = New(Config{})
	assert.NotNil(t, l)
	assert.NoError(t, l.Close())
}

// --- Registering and unregistering context field extractors ---

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
	assert.Equal(t, 0, countFields(t, key))

	assert.NotPanics(t, unregister)
	assert.NotPanics(t, unregister)
}

func TestRegisterContextExtractor_UnregisterIsolated(t *testing.T) {
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
	assert.Equal(t, 1, countFields(t, keyB))
}

func TestRegisterContextExtractor_Nil(t *testing.T) {
	unregister := RegisterContextExtractor(nil)
	require.NotNil(t, unregister)
	assert.NotPanics(t, unregister)
}

func TestRegisterContextExtractor_DuplicateProducesDuplicateFields(t *testing.T) {
	const key = "zz_test_dup"
	extractor := func(context.Context) []Field { return []Field{String(key, "v")} }

	unregister1 := RegisterContextExtractor(extractor)
	unregister2 := RegisterContextExtractor(extractor)
	t.Cleanup(func() { unregister1(); unregister2() })

	assert.Equal(t, 2, countFields(t, key))
	unregister1()
	assert.Equal(t, 1, countFields(t, key))
}

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

func TestNew_VolumeMode(t *testing.T) {
	dir := t.TempDir()
	l := New(Config{
		ServiceName: "svc",
		Mode:        "volume",
		Path:        dir,
	}).(*Logger)
	l.Info("volume test")
	require.NoError(t, l.Close())

	host, err := os.Hostname()
	require.NoError(t, err)
	content := readLogFile(t, filepath.Join(dir, "svc", host, "access.log"))
	assert.Contains(t, content, "volume test")
}

func TestNew_VolumeModeRequiresServiceName(t *testing.T) {
	dir := t.TempDir()
	l := New(Config{Mode: "volume", Path: dir}).(*Logger)
	l.Info("volume fallback")
	require.NoError(t, l.Close())

	content := readLogFile(t, filepath.Join(dir, "access.log"))
	assert.Contains(t, content, "volume fallback")
}

func TestLogger_MaxContentLength(t *testing.T) {
	l, dir := fileLogger(t, Config{
		Level:            "info",
		Encoding:         "json",
		MaxContentLength: 5,
	})
	l.Info("123456789")
	require.NoError(t, l.Close())

	content := readLogFile(t, filepath.Join(dir, "access.log"))
	assert.Contains(t, content, `"msg":"12345"`)
	assert.NotContains(t, content, "123456789")
}

// --- Buffered sinks ---

func TestLogger_BuffersUntilSync(t *testing.T) {
	l, dir := fileLogger(t, Config{Level: "info", Encoding: "json"})
	path := filepath.Join(dir, "access.log")

	l.Info("buffered entry")
	assert.Empty(t, readLogFile(t, path), "entries should stay buffered before Sync")

	require.NoError(t, l.Sync())
	assert.Contains(t, readLogFile(t, path), "buffered entry")
}

func TestLogger_FlushesBufferedEntriesOnClose(t *testing.T) {
	l, dir := fileLogger(t, Config{Level: "info", Encoding: "json"})
	l.Info("closed entry")
	require.NoError(t, l.Close())

	assert.Contains(t, readLogFile(t, filepath.Join(dir, "access.log")), "closed entry")
}

func TestLogger_PanicFlushesImmediately(t *testing.T) {
	l, dir := fileLogger(t, Config{Level: "debug", Encoding: "json"})

	assert.Panics(t, func() { l.Panic("boom") })

	// No Sync/Close yet: the panic entry must already be on disk. Panic sits
	// below Fatal, so it is written to error.log.
	assert.Contains(t, readLogFile(t, filepath.Join(dir, "error.log")), "boom")
}

func TestLogger_ConcurrentWritesStayLineIntact(t *testing.T) {
	l, dir := fileLogger(t, Config{Level: "info", Encoding: "json"})

	const goroutines, perGoroutine = 16, 200
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(g int) {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				l.Info("concurrent", Int("goroutine", g), Int("seq", i))
			}
		}(g)
	}
	wg.Wait()
	require.NoError(t, l.Sync())

	lines := strings.Split(strings.TrimSpace(readLogFile(t, filepath.Join(dir, "access.log"))), "\n")
	require.Len(t, lines, goroutines*perGoroutine)
	for _, line := range lines {
		require.NotEmpty(t, parseJSONLog(t, line).Msg)
	}
}

func TestGlobal_CloseIsIdempotent(t *testing.T) {
	prev := GetGlobal()
	l := New(Config{Level: "info", Mode: ModeFile, Path: t.TempDir()})
	SetGlobal(l)
	t.Cleanup(func() { SetGlobal(prev) })

	assert.NoError(t, Close())
	assert.NoError(t, Close())
	// Writes after Close are dropped rather than panicking.
	assert.NotPanics(t, func() { Info("after close") })
}
