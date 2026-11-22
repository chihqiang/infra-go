package logger

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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
)

// FieldType identifies the value carried by a Field.
type FieldType uint8

const (
	// StringType is a string field.
	StringType FieldType = iota
	// Int64Type is an integer field.
	Int64Type
	// Float64Type is a floating-point field.
	Float64Type
	// BoolType is a boolean field.
	BoolType
	// DurationType is a time.Duration field, stored in nanoseconds.
	DurationType
	// TimeType is a time.Time field.
	TimeType
	// ErrorType is an error field.
	ErrorType
	// AnyType is a field holding an arbitrary value.
	AnyType
)

// Field is a log field, used to pass key-value pairs into structured logging.
type Field struct {
	Key       string
	Type      FieldType
	String    string
	Integer   int64
	Interface any
}

// --- Field constructors ---

// String creates a log field of type string.
func String(key, val string) Field {
	return Field{Key: key, Type: StringType, String: val}
}

// Int creates a log field of type int.
func Int(key string, val int) Field {
	return Field{Key: key, Type: Int64Type, Integer: int64(val)}
}

// Int64 creates a log field of type int64.
func Int64(key string, val int64) Field {
	return Field{Key: key, Type: Int64Type, Integer: val}
}

// Float64 creates a log field of type float64.
func Float64(key string, val float64) Field {
	return Field{Key: key, Type: Float64Type, Interface: val}
}

// Bool creates a log field of type bool.
func Bool(key string, val bool) Field {
	integer := int64(0)
	if val {
		integer = 1
	}
	return Field{Key: key, Type: BoolType, Integer: integer}
}

// Duration creates a log field of type time.Duration.
func Duration(key string, val time.Duration) Field {
	return Field{Key: key, Type: DurationType, Integer: int64(val)}
}

// Time creates a log field of type time.Time.
func Time(key string, val time.Time) Field {
	return Field{Key: key, Type: TimeType, Interface: val}
}

// Any creates a log field holding a value of any type.
func Any(key string, val any) Field {
	return Field{Key: key, Type: AnyType, Interface: val}
}

// Err creates a log field of type error, with the fixed key name "error".
func Err(err error) Field {
	return Field{Key: "error", Type: ErrorType, Interface: err}
}

// Level is the log level type.
type Level int8

// Supported log levels.
const (
	DebugLevel  Level = -1
	InfoLevel   Level = 0
	WarnLevel   Level = 1
	ErrorLevel  Level = 2
	DPanicLevel Level = 3
	PanicLevel  Level = 4
	FatalLevel  Level = 5
)

// String returns the upper-case name of the level.
func (l Level) String() string {
	switch l {
	case DebugLevel:
		return "DEBUG"
	case InfoLevel:
		return "INFO"
	case WarnLevel:
		return "WARN"
	case ErrorLevel:
		return "ERROR"
	case DPanicLevel:
		return "DPANIC"
	case PanicLevel:
		return "PANIC"
	case FatalLevel:
		return "FATAL"
	default:
		return "UNKNOWN"
	}
}

// ILogger defines the core logger interface.
// Every implementation must provide the basic log methods and level checking.
type ILogger interface {
	Debug(msg string, fields ...Field)
	Info(msg string, fields ...Field)
	Warn(msg string, fields ...Field)
	Error(msg string, fields ...Field)
	Panic(msg string, fields ...Field)
	Fatal(msg string, fields ...Field)

	Debugf(format string, args ...any)
	Infof(format string, args ...any)
	Warnf(format string, args ...any)
	Errorf(format string, args ...any)
	Panicf(format string, args ...any)
	Fatalf(format string, args ...any)

	DebugCtx(ctx context.Context, msg string, fields ...Field)
	InfoCtx(ctx context.Context, msg string, fields ...Field)
	WarnCtx(ctx context.Context, msg string, fields ...Field)
	ErrorCtx(ctx context.Context, msg string, fields ...Field)
	PanicCtx(ctx context.Context, msg string, fields ...Field)
	FatalCtx(ctx context.Context, msg string, fields ...Field)

	DebugfCtx(ctx context.Context, format string, args ...any)
	InfofCtx(ctx context.Context, format string, args ...any)
	WarnfCtx(ctx context.Context, format string, args ...any)
	ErrorfCtx(ctx context.Context, format string, args ...any)
	PanicfCtx(ctx context.Context, format string, args ...any)
	FatalfCtx(ctx context.Context, format string, args ...any)

	// Sync flushes buffered entries and fsyncs file outputs. It keeps the
	// logger usable, so call it when logs must be on disk before another
	// operation continues.
	Sync() error

	// Close flushes buffered entries, stops the background flusher and closes
	// the files the logger owns. It is idempotent, and writes after Close are
	// dropped. Implementations that own no resources return nil.
	Close() error
}

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

// Logger is the default implementation of the ILogger interface.
type Logger struct {
	config Config
	level  Level
	enc    encoder
	out    sinks

	lastStack atomic.Int64
	closed    atomic.Bool

	flushStop chan struct{}
	flushDone chan struct{}
}

// New creates and returns an ILogger from the configuration.
func New(cfg Config) ILogger {
	c := cfg.withDefaults()

	timeFormat := c.TimeFormat
	if timeFormat == "" {
		timeFormat = defaultTimeFormat
	}
	l := &Logger{
		config: c,
		level:  parseLevel(c.Level),
		enc:    newEncoder(c.Encoding, timeFormat),
		out:    newSinks(c),
	}
	l.startFlusher()
	return l
}

// Default returns a Logger that uses the default configuration.
func Default() ILogger {
	return New(Config{})
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
	if !l.enabled(level) || l.closed.Load() {
		return
	}
	if l.config.MaxContentLength > 0 && uint32(len(msg)) > l.config.MaxContentLength {
		msg = msg[:l.config.MaxContentLength]
	}

	// Encode outside any lock: caller lookup and JSON/plain building are the
	// expensive part and are safe to run in parallel. The buffer is pooled
	// because each entry is copied into the sink's buffer before Write returns.
	e := entry{
		level:  level,
		now:    time.Now(),
		caller: callerInfo(),
		app:    l.config.ServiceName,
		msg:    msg,
		fields: fields,
	}
	if level >= ErrorLevel {
		e.stack = l.stackFor(e.now)
	}

	buf := encodeBufPool.Get().(*bytes.Buffer)
	buf.Reset()
	l.enc.encodeInto(buf, e)

	writer := l.out.pick(level)
	_, err := writer.Write(buf.Bytes())
	encodeBufPool.Put(buf)
	if err != nil && !errors.Is(err, errSinkClosed) {
		_, _ = fmt.Fprintf(os.Stderr, "logger: write log: %v\n", err)
	}
	// Panic and fatal entries must reach the sink before the process panics or
	// exits, so they bypass batching.
	if level >= PanicLevel {
		_ = writer.Flush()
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

// encodeBufPool reuses the per-entry encoding buffer. Entries are copied into
// the sink before the buffer is returned, so pooling is safe.
var encodeBufPool = sync.Pool{New: func() any { return new(bytes.Buffer) }}

// stackFor returns the current stack trace at most once per configured
// cooldown, so concurrent errors do not all dump a stack.
func (l *Logger) stackFor(now time.Time) string {
	cooldown := int64(time.Duration(l.config.StackCooldownMillis) * time.Millisecond)
	last := l.lastStack.Load()
	if cooldown > 0 && last != 0 && now.UnixNano()-last < cooldown {
		return ""
	}
	if cooldown > 0 && !l.lastStack.CompareAndSwap(last, now.UnixNano()) {
		return ""
	}
	return string(debug.Stack())
}

// Sync flushes the buffered entries and fsyncs the file outputs.
func (l *Logger) Sync() error {
	var errs []error
	for _, sink := range l.out.all() {
		if err := sink.Flush(); err != nil {
			errs = append(errs, err)
		}
	}
	for _, closer := range l.out.closers {
		if syncer, ok := closer.(interface{ Sync() error }); ok {
			if err := syncer.Sync(); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

// Close shuts the logger down, flushing buffers and closing file outputs.
func (l *Logger) Close() error {
	if !l.closed.CompareAndSwap(false, true) {
		return nil
	}
	l.stopFlusher()

	var errs []error
	for _, sink := range l.out.all() {
		if err := sink.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	for _, closer := range l.out.closers {
		if err := closer.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// startFlusher bounds how long an entry stays buffered when traffic is low.
func (l *Logger) startFlusher() {
	l.flushStop = make(chan struct{})
	l.flushDone = make(chan struct{})
	go func() {
		defer close(l.flushDone)
		ticker := time.NewTicker(flushInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				for _, sink := range l.out.all() {
					_ = sink.Flush()
				}
			case <-l.flushStop:
				return
			}
		}
	}()
}

// stopFlusher stops the background flush loop and waits for it to exit.
func (l *Logger) stopFlusher() {
	if l.flushStop == nil {
		return
	}
	close(l.flushStop)
	<-l.flushDone
	l.flushStop = nil
}
