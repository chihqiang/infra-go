package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chihqiang/ten-go/core/mapping"
)

const (
	// JSONEncoding outputs JSON.
	JSONEncoding = "json"
	// ConsoleEncoding outputs a plain, human-readable line. It is the value used by
	// Encoding="plain" in Config.
	ConsoleEncoding = "plain"

	defaultTimeFormat = "2006-01-02T15:04:05.000Z07:00"
)

// ContextExtractor is a function that extracts log fields from a context.
type ContextExtractor func(ctx context.Context) []Field

// registeredExtractor is a registered context field extractor.
type registeredExtractor struct {
	fn      ContextExtractor
	removed atomic.Bool
}

var (
	contextExtractors []*registeredExtractor
	extractorsMu      sync.RWMutex
)

// RegisterContextExtractor registers a context field extractor and returns an unregister
// function. The unregister function is idempotent; a nil extractor is ignored.
func RegisterContextExtractor(extractor ContextExtractor) (unregister func()) {
	if extractor == nil {
		return func() {}
	}

	entry := &registeredExtractor{fn: extractor}
	extractorsMu.Lock()
	contextExtractors = append(contextExtractors, entry)
	extractorsMu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			entry.removed.Store(true)

			extractorsMu.Lock()
			defer extractorsMu.Unlock()
			live := make([]*registeredExtractor, 0, len(contextExtractors))
			for _, e := range contextExtractors {
				if !e.removed.Load() {
					live = append(live, e)
				}
			}
			contextExtractors = live
		})
	}
}

func extractContextFields(ctx context.Context) []Field {
	extractorsMu.RLock()
	extractors := contextExtractors
	extractorsMu.RUnlock()

	fields := make([]Field, 0, len(extractors)*2)
	for _, e := range extractors {
		if e.removed.Load() {
			continue
		}
		fields = append(fields, e.fn(ctx)...)
	}
	return fields
}

// Config is the log configuration, following go-zero's LogConf layout.
type Config struct {
	// ServiceName is the service name; volume mode uses it to build the log directory.
	ServiceName string `json:",optional"`
	// Mode selects console, file or volume output; default is console.
	Mode string `json:",default=console,options=[console,file,volume]"`
	// Encoding selects json or plain output; default is json.
	Encoding string `json:",default=json,options=[json,plain]"`
	// TimeFormat is the timestamp layout; an empty value uses ISO8601.
	TimeFormat string `json:",optional"`
	// Path is the log directory for file and volume modes; default is "logs".
	Path string `json:",default=logs"`
	// Level is one of debug, info, error or severe; default is info. "severe" keeps only
	// fatal entries, which are the ones written to severe.log.
	Level string `json:",default=info,options=[debug,info,error,severe]"`
	// MaxContentLength truncates the message to this many bytes; zero means no limit.
	MaxContentLength uint32 `json:",optional"`
	// Compress reports whether rotated log files are gzip-compressed.
	Compress bool `json:",optional"`
	// Stat enables statistics in go-zero's logx; kept for configuration compatibility.
	Stat bool `json:",default=true"`
	// KeepDays is how many days backups are kept; zero keeps them forever.
	KeepDays int `json:",optional"`
	// StackCooldownMillis limits how often stack traces are attached; default 100ms.
	StackCooldownMillis int `json:",default=100"`
	// MaxBackups is the maximum number of backups kept; zero keeps all.
	MaxBackups int `json:",default=0"`
	// MaxSize is the maximum file size in MB for size rotation; zero means no limit.
	MaxSize int `json:",default=0"`
	// Rotation selects daily or size rotation; default is daily.
	Rotation string `json:",default=daily,options=[daily,size]"`
}

// Logger is the default implementation of the ILogger interface.
type Logger struct {
	mu          sync.Mutex
	config      Config
	level       Level
	timeFormat  string
	access      io.Writer
	errorWriter io.Writer
	severe      io.Writer
	closers     []io.Closer
	lastStack   time.Time
	closed      bool
}

// New creates and returns an ILogger from the configuration.
func New(cfg Config) ILogger {
	c := fillDefault(cfg)

	l := &Logger{
		config:     c,
		level:      parseLevel(c.Level),
		timeFormat: c.TimeFormat,
	}
	if l.timeFormat == "" {
		l.timeFormat = defaultTimeFormat
	}
	l.setupWriters(c)
	return l
}

// Default returns a Logger that uses the default configuration.
func Default() ILogger {
	return New(Config{})
}

func (l *Logger) setupWriters(c Config) {
	switch c.Mode {
	case "file":
		l.setupFileWriters(c, c.Path)
	case "volume":
		if c.ServiceName == "" {
			fmt.Fprintln(os.Stderr, "logger: volume mode requires ServiceName; using file mode")
			l.setupFileWriters(c, c.Path)
			return
		}
		host, _ := os.Hostname()
		l.setupFileWriters(c, filepath.Join(c.Path, c.ServiceName, host))
	default:
		l.access = os.Stdout
		l.errorWriter = os.Stderr
		l.severe = os.Stderr
	}
}

func (l *Logger) setupFileWriters(c Config, dir string) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "logger: failed to create log directory %q: %v\n", dir, err)
		l.access = os.Stderr
		l.errorWriter = os.Stderr
		l.severe = os.Stderr
		return
	}

	open := func(name string) io.Writer {
		writer, err := newRotatingWriter(filepath.Join(dir, name), c)
		if err != nil {
			fmt.Fprintf(os.Stderr, "logger: failed to open %q: %v\n", name, err)
			return os.Stderr
		}
		l.closers = append(l.closers, writer)
		return writer
	}
	l.access = open("access.log")
	l.errorWriter = open("error.log")
	l.severe = open("severe.log")
}

// Debug logs one structured entry at Debug level.
func (l *Logger) Debug(msg string, fields ...Field) { l.log(DebugLevel, msg, fields) }

// Info logs one structured entry at Info level.
func (l *Logger) Info(msg string, fields ...Field) { l.log(InfoLevel, msg, fields) }

// Warn logs one structured entry at Warn level.
func (l *Logger) Warn(msg string, fields ...Field) { l.log(WarnLevel, msg, fields) }

// Error logs one structured entry at Error level.
func (l *Logger) Error(msg string, fields ...Field) { l.log(ErrorLevel, msg, fields) }

// Panic logs at Panic level and then panics.
func (l *Logger) Panic(msg string, fields ...Field) {
	l.log(PanicLevel, msg, fields)
	panic(msg)
}

// Fatal logs at Fatal level and then calls os.Exit(1).
func (l *Logger) Fatal(msg string, fields ...Field) {
	l.log(FatalLevel, msg, fields)
	_ = l.Sync()
	os.Exit(1)
}

// Debugf logs a formatted entry at Debug level.
func (l *Logger) Debugf(format string, args ...any) {
	if l.enabled(DebugLevel) {
		l.log(DebugLevel, fmt.Sprintf(format, args...), nil)
	}
}

// Infof logs a formatted entry at Info level.
func (l *Logger) Infof(format string, args ...any) {
	if l.enabled(InfoLevel) {
		l.log(InfoLevel, fmt.Sprintf(format, args...), nil)
	}
}

// Warnf logs a formatted entry at Warn level.
func (l *Logger) Warnf(format string, args ...any) {
	if l.enabled(WarnLevel) {
		l.log(WarnLevel, fmt.Sprintf(format, args...), nil)
	}
}

// Errorf logs a formatted entry at Error level.
func (l *Logger) Errorf(format string, args ...any) {
	if l.enabled(ErrorLevel) {
		l.log(ErrorLevel, fmt.Sprintf(format, args...), nil)
	}
}

// Panicf logs a formatted entry at Panic level and then panics.
func (l *Logger) Panicf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	l.log(PanicLevel, msg, nil)
	panic(msg)
}

// Fatalf logs a formatted entry at Fatal level and then calls os.Exit(1).
func (l *Logger) Fatalf(format string, args ...any) {
	l.log(FatalLevel, fmt.Sprintf(format, args...), nil)
	_ = l.Sync()
	os.Exit(1)
}

// DebugCtx logs at Debug level, merging in the extractor fields of ctx.
func (l *Logger) DebugCtx(ctx context.Context, msg string, fields ...Field) {
	if !l.enabled(DebugLevel) {
		return
	}
	l.log(DebugLevel, msg, append(extractContextFields(ctx), fields...))
}

// InfoCtx logs at Info level, merging in the extractor fields of ctx.
func (l *Logger) InfoCtx(ctx context.Context, msg string, fields ...Field) {
	if !l.enabled(InfoLevel) {
		return
	}
	l.log(InfoLevel, msg, append(extractContextFields(ctx), fields...))
}

// WarnCtx logs at Warn level, merging in the extractor fields of ctx.
func (l *Logger) WarnCtx(ctx context.Context, msg string, fields ...Field) {
	if !l.enabled(WarnLevel) {
		return
	}
	l.log(WarnLevel, msg, append(extractContextFields(ctx), fields...))
}

// ErrorCtx logs at Error level, merging in the extractor fields of ctx.
func (l *Logger) ErrorCtx(ctx context.Context, msg string, fields ...Field) {
	if !l.enabled(ErrorLevel) {
		return
	}
	l.log(ErrorLevel, msg, append(extractContextFields(ctx), fields...))
}

// PanicCtx logs at Panic level, then panics, merging in the extractor fields of ctx.
func (l *Logger) PanicCtx(ctx context.Context, msg string, fields ...Field) {
	l.log(PanicLevel, msg, append(extractContextFields(ctx), fields...))
	panic(msg)
}

// FatalCtx logs at Fatal level, then calls os.Exit(1), merging in the extractor fields.
func (l *Logger) FatalCtx(ctx context.Context, msg string, fields ...Field) {
	l.log(FatalLevel, msg, append(extractContextFields(ctx), fields...))
	_ = l.Sync()
	os.Exit(1)
}

// DebugfCtx logs a formatted entry at Debug level, merging in the extractor fields of ctx.
func (l *Logger) DebugfCtx(ctx context.Context, format string, args ...any) {
	if !l.enabled(DebugLevel) {
		return
	}
	l.log(DebugLevel, fmt.Sprintf(format, args...), extractContextFields(ctx))
}

// InfofCtx logs a formatted entry at Info level, merging in the extractor fields of ctx.
func (l *Logger) InfofCtx(ctx context.Context, format string, args ...any) {
	if !l.enabled(InfoLevel) {
		return
	}
	l.log(InfoLevel, fmt.Sprintf(format, args...), extractContextFields(ctx))
}

// WarnfCtx logs a formatted entry at Warn level, merging in the extractor fields of ctx.
func (l *Logger) WarnfCtx(ctx context.Context, format string, args ...any) {
	if !l.enabled(WarnLevel) {
		return
	}
	l.log(WarnLevel, fmt.Sprintf(format, args...), extractContextFields(ctx))
}

// ErrorfCtx logs a formatted entry at Error level, merging in the extractor fields of ctx.
func (l *Logger) ErrorfCtx(ctx context.Context, format string, args ...any) {
	if !l.enabled(ErrorLevel) {
		return
	}
	l.log(ErrorLevel, fmt.Sprintf(format, args...), extractContextFields(ctx))
}

// PanicfCtx logs a formatted entry at Panic level, then panics.
func (l *Logger) PanicfCtx(ctx context.Context, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	l.log(PanicLevel, msg, extractContextFields(ctx))
	panic(msg)
}

// FatalfCtx logs a formatted entry at Fatal level, then calls os.Exit(1).
func (l *Logger) FatalfCtx(ctx context.Context, format string, args ...any) {
	l.log(FatalLevel, fmt.Sprintf(format, args...), extractContextFields(ctx))
	_ = l.Sync()
	os.Exit(1)
}

func (l *Logger) enabled(level Level) bool {
	return level >= l.level
}

func (l *Logger) log(level Level, msg string, fields []Field) {
	if !l.enabled(level) {
		return
	}
	if l.config.MaxContentLength > 0 && uint32(len(msg)) > l.config.MaxContentLength {
		msg = msg[:l.config.MaxContentLength]
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return
	}

	now := time.Now()
	entry := l.encode(level, now, msg, fields)

	writer := l.access
	switch {
	case level >= FatalLevel:
		writer = l.severe
	case level >= ErrorLevel:
		writer = l.errorWriter
	}
	if writer == nil {
		writer = os.Stderr
	}
	if _, err := writer.Write(entry); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "logger: write log: %v\n", err)
	}
}

// loggerPackagePath is the function-name prefix shared by this package's code.
var loggerPackagePath = reflect.TypeOf(Level(0)).PkgPath() + "."

// callerInfo reports the file and line of the first stack frame outside this package, so
// the caller is consistent whether logging goes through a Logger method or a
// package-level helper in log.go. Frames from this package's tests are reported so that
// tests can assert their own call site.
func callerInfo() string {
	var pcs [16]uintptr
	n := runtime.Callers(2, pcs[:])
	frames := runtime.CallersFrames(pcs[:n])
	for {
		frame, more := frames.Next()
		if !strings.HasPrefix(frame.Function, loggerPackagePath) ||
			strings.HasSuffix(frame.File, "_test.go") {
			return filepath.Base(frame.File) + ":" + strconv.Itoa(frame.Line)
		}
		if !more {
			return ""
		}
	}
}

func (l *Logger) encode(level Level, now time.Time, msg string, fields []Field) []byte {
	caller := callerInfo()

	stack := ""
	if level >= ErrorLevel {
		cooldown := time.Duration(l.config.StackCooldownMillis) * time.Millisecond
		if cooldown <= 0 || l.lastStack.IsZero() || now.Sub(l.lastStack) >= cooldown {
			stack = string(debug.Stack())
			l.lastStack = now
		}
	}

	if l.config.Encoding == ConsoleEncoding {
		return l.encodePlain(level, now, caller, msg, fields, stack)
	}
	return l.encodeJSON(level, now, caller, msg, fields, stack)
}

func (l *Logger) encodeJSON(level Level, now time.Time, caller, msg string, fields []Field, stack string) []byte {
	var buf bytes.Buffer
	buf.WriteByte('{')
	first := true
	writePair := func(key string, value []byte) {
		if !first {
			buf.WriteByte(',')
		}
		first = false
		buf.WriteString(jsonString(key))
		buf.WriteByte(':')
		buf.Write(value)
	}
	writeStringPair := func(key, value string) {
		writePair(key, []byte(jsonString(value)))
	}

	writeStringPair("level", level.String())
	writeStringPair("time", now.Format(l.timeFormat))
	if caller != "" {
		writeStringPair("caller", caller)
	}
	if l.config.ServiceName != "" {
		writeStringPair("app", l.config.ServiceName)
	}
	writeStringPair("msg", msg)
	for _, field := range fields {
		writePair(field.Key, l.fieldJSON(field))
	}
	if stack != "" {
		writeStringPair("stacktrace", stack)
	}
	buf.WriteString("}\n")
	return buf.Bytes()
}

func (l *Logger) encodePlain(level Level, now time.Time, caller, msg string, fields []Field, stack string) []byte {
	var buf bytes.Buffer
	buf.WriteString(now.Format(l.timeFormat))
	buf.WriteByte(' ')
	buf.WriteString(level.String())
	if caller != "" {
		buf.WriteByte(' ')
		buf.WriteString(caller)
	}
	if l.config.ServiceName != "" {
		buf.WriteString(" app=")
		buf.WriteString(l.config.ServiceName)
	}
	buf.WriteByte(' ')
	buf.WriteString(msg)
	for _, field := range fields {
		buf.WriteByte(' ')
		buf.WriteString(field.Key)
		buf.WriteByte('=')
		buf.WriteString(fieldText(field, l.timeFormat))
	}
	if stack != "" {
		buf.WriteString(" stacktrace=")
		buf.WriteString(stack)
	}
	buf.WriteByte('\n')
	return buf.Bytes()
}

func (l *Logger) fieldJSON(field Field) []byte {
	switch field.Type {
	case StringType:
		return []byte(jsonString(field.String))
	case Int64Type:
		return []byte(strconv.FormatInt(field.Integer, 10))
	case Float64Type:
		return marshalJSON(field.Interface)
	case BoolType:
		if field.Integer != 0 {
			return []byte("true")
		}
		return []byte("false")
	case DurationType:
		millis := int64(time.Duration(field.Integer) / time.Millisecond)
		return []byte(strconv.FormatInt(millis, 10))
	case TimeType:
		if t, ok := field.Interface.(time.Time); ok {
			return []byte(jsonString(t.Format(l.timeFormat)))
		}
		return marshalJSON(field.Interface)
	case ErrorType:
		if field.Interface == nil {
			return []byte("null")
		}
		if err, ok := field.Interface.(error); ok {
			return []byte(jsonString(err.Error()))
		}
		return marshalJSON(field.Interface)
	default:
		return marshalJSON(field.Interface)
	}
}

func fieldText(field Field, timeFormat string) string {
	switch field.Type {
	case StringType:
		return field.String
	case Int64Type:
		return strconv.FormatInt(field.Integer, 10)
	case Float64Type:
		return fmt.Sprint(field.Interface)
	case BoolType:
		return strconv.FormatBool(field.Integer != 0)
	case DurationType:
		return time.Duration(field.Integer).String()
	case TimeType:
		if t, ok := field.Interface.(time.Time); ok {
			return t.Format(timeFormat)
		}
		return fmt.Sprint(field.Interface)
	case ErrorType:
		if field.Interface == nil {
			return "<nil>"
		}
		if err, ok := field.Interface.(error); ok {
			return err.Error()
		}
		return fmt.Sprint(field.Interface)
	default:
		return fmt.Sprint(field.Interface)
	}
}

// Sync flushes the active outputs.
func (l *Logger) Sync() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	var errs []error
	for _, w := range []io.Writer{l.access, l.errorWriter, l.severe} {
		if syncer, ok := w.(interface{ Sync() error }); ok {
			if err := syncer.Sync(); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

// Close shuts the logger down, flushing and closing file outputs.
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true

	var errs []error
	for _, w := range []io.Writer{l.access, l.errorWriter, l.severe} {
		if syncer, ok := w.(interface{ Sync() error }); ok {
			errs = append(errs, syncer.Sync())
		}
	}
	for _, c := range l.closers {
		errs = append(errs, c.Close())
	}
	return errors.Join(errs...)
}

// --- Internal functions ---

func fillDefault(cfg Config) Config {
	var c Config
	mapping.MustFillAndOverride(&c, cfg)
	if c.TimeFormat == "" {
		c.TimeFormat = defaultTimeFormat
	}
	return c
}

func parseLevel(level string) Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return DebugLevel
	case "warn", "warning":
		return WarnLevel
	case "error":
		return ErrorLevel
	case "severe":
		return FatalLevel
	case "dpanic":
		return DPanicLevel
	case "panic":
		return PanicLevel
	case "fatal":
		return FatalLevel
	default:
		return InfoLevel
	}
}

func jsonString(s string) string {
	data, err := json.Marshal(s)
	if err != nil {
		return `""`
	}
	return string(data)
}

func marshalJSON(v any) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		return []byte(jsonString(fmt.Sprint(v)))
	}
	return data
}
