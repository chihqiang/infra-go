package logger

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/chihqiang/infra-go/mapping"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

// Encoding is the log encoding format type.
type Encoding string

const (
	// JSONEncoding outputs JSON.
	JSONEncoding Encoding = "json"
	// ConsoleEncoding outputs the console format, which is human readable.
	ConsoleEncoding Encoding = "console"
)

// ContextExtractor is a function that extracts log fields from a context.
type ContextExtractor func(ctx context.Context) []Field

// registeredExtractor is a registered context field extractor.
//
// removed is an atomic flag rather than a slice deletion: the read path
// (extractContextFields) walks the backing array outside the lock, so deleting or moving
// elements in place would race with a goroutine that is iterating it.
type registeredExtractor struct {
	fn      ContextExtractor
	removed atomic.Bool
}

var (
	contextExtractors []*registeredExtractor
	extractorsMu      sync.RWMutex
)

// RegisterContextExtractor registers a context field extractor and returns an unregister
// function.
//
// Historical problem: registration only **appended** and had no unregister entry point,
// while function values are not comparable in Go, so registration could not be
// de-duplicated. Whenever the initialisation flow ran more than once (retries, multi-stage
// configuration, test reuse) the same extractor was registered several times, printing the
// same fields over and over on every log entry. The returned unregister function solves
// this: it is idempotent, so repeated calls take effect only once.
//
//	unregister := logger.RegisterContextExtractor(myExtractor)
//	defer unregister() // undo it when no longer needed
//
// A nil extractor is not registered and the returned unregister function is a no-op.
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

			// Also compact the slice, so that repeated register/unregister cycles over a
			// long period cannot grow it without bound.
			// Replacing the slice header wholesale is safe: the read path has already
			// copied the old header, so the old backing array is not modified and the
			// element is still skipped through its removed flag.
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

	// Pre-allocate the capacity to avoid repeated growth
	fields := make([]Field, 0, len(extractors)*2)
	for _, e := range extractors {
		if e.removed.Load() {
			continue
		}
		fields = append(fields, e.fn(ctx)...)
	}
	return fields
}

// RotationConfig configures log file rotation.
// It is implemented on top of lumberjack and supports splitting by file size and keeping
// files for a number of days.
type RotationConfig struct {
	// MaxSize is the maximum size of a single log file in MB; defaults to 100MB.
	MaxSize int `json:",default=100"`
	// MaxBackups is the maximum number of old log files to keep; defaults to 7.
	MaxBackups int `json:",default=7"`
	// MaxAge is the maximum number of days old log files are kept; defaults to 30.
	MaxAge int `json:",default=30"`
	// Compress reports whether old log files are compressed (gzip); defaults to false.
	Compress bool `json:",optional"`
	// LocalTime reports whether backup files are named using local time; defaults to true.
	LocalTime bool `json:",default=true"`
}

// Config is the log configuration.
type Config struct {
	// Level is the log level; defaults to InfoLevel (zapcore.InfoLevel = 0).
	Level Level `json:",default=0"`
	// Encoding is the encoding format; defaults to JSONEncoding.
	Encoding Encoding `json:",default=json"`
	// Output lists the output targets, which may be "stdout", "stderr" or a file path;
	// defaults to ["stdout"].
	Output []string `json:",default=[stdout]"`
	// ErrorOutput is the error output target; defaults to "stderr".
	ErrorOutput string `json:",default=stderr"`
	// Caller reports whether caller information (file name and line number) is recorded;
	// defaults to true.
	Caller bool `json:",default=true"`
	// Stacktrace reports whether a stack trace is recorded at Error level and above;
	// defaults to false.
	Stacktrace bool `json:",optional"`
	// TimeLayout is the time format layout; defaults to ISO8601.
	TimeLayout string `json:",default=2006-01-02T15:04:05.000Z07:00"`
	// AppName is the application name, emitted as a fixed field; defaults to empty.
	AppName string `json:",optional"`
	// Rotation is the log file rotation configuration.
	Rotation RotationConfig
}

// Logger is the default implementation of the ILogger interface and wraps a zap.Logger.
// Create it with New / Default; it is normally used through the ILogger interface, and
// Close flushes the buffers and closes the outputs.
type Logger struct {
	zap     *zap.Logger
	sugar   *zap.SugaredLogger
	config  Config
	closers []io.Closer
}

// New creates and returns an ILogger from the configuration.
func New(cfg Config) ILogger {
	c := fillDefault(cfg)

	encoder := newEncoder(c)
	levelEnabler := zap.LevelEnablerFunc(func(lvl Level) bool {
		return lvl >= c.Level
	})

	// Build the output writers
	cores := make([]zapcore.Core, 0, len(c.Output))
	var closers []io.Closer
	for _, output := range c.Output {
		w, closer, err := openOutput(output, c.Rotation)
		if err != nil {
			fmt.Fprintf(os.Stderr, "logger: failed to open output %q: %v\n", output, err)
			w = os.Stderr
		} else if closer != nil {
			closers = append(closers, closer)
		}
		cores = append(cores, zapcore.NewCore(encoder, zapcore.AddSync(w), levelEnabler))
	}

	// Build the error output writer
	errW, _, err := openOutput(c.ErrorOutput, c.Rotation)
	if err != nil {
		errW = os.Stderr
	}

	opts := buildOptions(c, errW)
	zapLogger := zap.New(zapcore.NewTee(cores...), opts...)

	if c.AppName != "" {
		zapLogger = zapLogger.With(zap.String("app", c.AppName))
	}

	return &Logger{
		zap:     zapLogger,
		sugar:   zapLogger.Sugar(),
		config:  c,
		closers: closers,
	}
}

// Default returns a Logger that uses the default configuration.
func Default() ILogger {
	return New(Config{})
}

// --- Structured logging methods ---

// Debug logs one structured entry at Debug level.
func (l *Logger) Debug(msg string, fields ...Field) { l.zap.Debug(msg, fields...) }

// Info logs one structured entry at Info level.
func (l *Logger) Info(msg string, fields ...Field) { l.zap.Info(msg, fields...) }

// Warn logs one structured entry at Warn level.
func (l *Logger) Warn(msg string, fields ...Field) { l.zap.Warn(msg, fields...) }

// Error logs one structured entry at Error level.
func (l *Logger) Error(msg string, fields ...Field) { l.zap.Error(msg, fields...) }

// Panic logs at Panic level and then panics.
func (l *Logger) Panic(msg string, fields ...Field) { l.zap.Panic(msg, fields...) }

// Fatal logs at Fatal level and then calls os.Exit(1).
func (l *Logger) Fatal(msg string, fields ...Field) { l.zap.Fatal(msg, fields...) }

// --- Formatted logging methods ---
//
// The formatted logging methods skip fmt.Sprintf when the level is not enabled, avoiding
// pointless formatting work. The Panic/Fatal variants are kept as they are: zap's
// Panic/Fatal trigger panic/os.Exit even when the level is disabled, and that behaviour is
// not changed here.

// Debugf logs a formatted entry at Debug level.
func (l *Logger) Debugf(format string, args ...any) {
	if l.zap.Core().Enabled(zapcore.DebugLevel) {
		l.zap.Debug(fmt.Sprintf(format, args...))
	}
}

// Infof logs a formatted entry at Info level.
func (l *Logger) Infof(format string, args ...any) {
	if l.zap.Core().Enabled(zapcore.InfoLevel) {
		l.zap.Info(fmt.Sprintf(format, args...))
	}
}

// Warnf logs a formatted entry at Warn level.
func (l *Logger) Warnf(format string, args ...any) {
	if l.zap.Core().Enabled(zapcore.WarnLevel) {
		l.zap.Warn(fmt.Sprintf(format, args...))
	}
}

// Errorf logs a formatted entry at Error level.
func (l *Logger) Errorf(format string, args ...any) {
	if l.zap.Core().Enabled(zapcore.ErrorLevel) {
		l.zap.Error(fmt.Sprintf(format, args...))
	}
}

// Panicf logs a formatted entry at Panic level and then panics.
func (l *Logger) Panicf(format string, args ...any) { l.zap.Panic(fmt.Sprintf(format, args...)) }

// Fatalf logs a formatted entry at Fatal level and then calls os.Exit(1).
func (l *Logger) Fatalf(format string, args ...any) { l.zap.Fatal(fmt.Sprintf(format, args...)) }

// --- Structured logging methods with context ---
//
// The context-aware methods merge the context extractor fields registered in the context
// (such as request_id) into the log entry, which helps with tracing. They check the level
// first and return immediately when it is disabled, avoiding the cost of extracting fields.

// DebugCtx logs at Debug level, merging in the extractor fields of ctx.
func (l *Logger) DebugCtx(ctx context.Context, msg string, fields ...Field) {
	if !l.zap.Core().Enabled(zapcore.DebugLevel) {
		return
	}
	l.zap.Debug(msg, append(extractContextFields(ctx), fields...)...)
}

// InfoCtx logs at Info level, merging in the extractor fields of ctx.
func (l *Logger) InfoCtx(ctx context.Context, msg string, fields ...Field) {
	if !l.zap.Core().Enabled(zapcore.InfoLevel) {
		return
	}
	l.zap.Info(msg, append(extractContextFields(ctx), fields...)...)
}

// WarnCtx logs at Warn level, merging in the extractor fields of ctx.
func (l *Logger) WarnCtx(ctx context.Context, msg string, fields ...Field) {
	if !l.zap.Core().Enabled(zapcore.WarnLevel) {
		return
	}
	l.zap.Warn(msg, append(extractContextFields(ctx), fields...)...)
}

// ErrorCtx logs at Error level, merging in the extractor fields of ctx.
func (l *Logger) ErrorCtx(ctx context.Context, msg string, fields ...Field) {
	if !l.zap.Core().Enabled(zapcore.ErrorLevel) {
		return
	}
	l.zap.Error(msg, append(extractContextFields(ctx), fields...)...)
}

// PanicCtx logs at Panic level, then panics, merging in the extractor fields of ctx.
func (l *Logger) PanicCtx(ctx context.Context, msg string, fields ...Field) {
	l.zap.With(extractContextFields(ctx)...).Panic(msg, fields...)
}

// FatalCtx logs at Fatal level, then calls os.Exit(1), merging in the extractor fields of
// ctx.
func (l *Logger) FatalCtx(ctx context.Context, msg string, fields ...Field) {
	l.zap.With(extractContextFields(ctx)...).Fatal(msg, fields...)
}

// --- Formatted logging methods with context ---

// DebugfCtx logs a formatted entry at Debug level, merging in the extractor fields of ctx.
func (l *Logger) DebugfCtx(ctx context.Context, format string, args ...any) {
	if !l.zap.Core().Enabled(zapcore.DebugLevel) {
		return
	}
	l.zap.Debug(fmt.Sprintf(format, args...), extractContextFields(ctx)...)
}

// InfofCtx logs a formatted entry at Info level, merging in the extractor fields of ctx.
func (l *Logger) InfofCtx(ctx context.Context, format string, args ...any) {
	if !l.zap.Core().Enabled(zapcore.InfoLevel) {
		return
	}
	l.zap.Info(fmt.Sprintf(format, args...), extractContextFields(ctx)...)
}

// WarnfCtx logs a formatted entry at Warn level, merging in the extractor fields of ctx.
func (l *Logger) WarnfCtx(ctx context.Context, format string, args ...any) {
	if !l.zap.Core().Enabled(zapcore.WarnLevel) {
		return
	}
	l.zap.Warn(fmt.Sprintf(format, args...), extractContextFields(ctx)...)
}

// ErrorfCtx logs a formatted entry at Error level, merging in the extractor fields of ctx.
func (l *Logger) ErrorfCtx(ctx context.Context, format string, args ...any) {
	if !l.zap.Core().Enabled(zapcore.ErrorLevel) {
		return
	}
	l.zap.Error(fmt.Sprintf(format, args...), extractContextFields(ctx)...)
}

// PanicfCtx logs a formatted entry at Panic level, then panics, merging in the extractor
// fields of ctx.
func (l *Logger) PanicfCtx(ctx context.Context, format string, args ...any) {
	l.zap.With(extractContextFields(ctx)...).Panic(fmt.Sprintf(format, args...))
}

// FatalfCtx logs a formatted entry at Fatal level, then calls os.Exit(1), merging in the
// extractor fields of ctx.
func (l *Logger) FatalfCtx(ctx context.Context, format string, args ...any) {
	l.zap.With(extractContextFields(ctx)...).Fatal(fmt.Sprintf(format, args...))
}

// --- Sync ---

// Sync flushes buffered log entries.
func (l *Logger) Sync() error {
	return l.zap.Sync()
}

// --- Extra methods (not part of the interface) ---

// Close shuts the logger down, flushing buffers and closing file outputs.
func (l *Logger) Close() error {
	var errs []string
	if err := l.zap.Sync(); err != nil {
		errs = append(errs, err.Error())
	}
	for _, c := range l.closers {
		if err := c.Close(); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("logger close errors: %s", strings.Join(errs, "; "))
	}
	return nil
}

// --- Internal functions ---

// fillDefault fills in the default values and then overrides them with the non-zero fields
// of the user configuration, using mapping.FillAndOverride for both steps.
// An empty AppName is also treated as a valid value (it always overrides); this is achieved
// by the optional tag with no default.
func fillDefault(cfg Config) Config {
	var c Config
	mapping.MustFillAndOverride(&c, cfg)
	return c
}

func newEncoder(c Config) zapcore.Encoder {
	encoderCfg := zapcore.EncoderConfig{
		TimeKey:        "time",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		FunctionKey:    zapcore.OmitKey,
		MessageKey:     "msg",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.CapitalLevelEncoder,
		EncodeTime:     zapcore.TimeEncoderOfLayout(c.TimeLayout),
		EncodeDuration: zapcore.MillisDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}

	switch c.Encoding {
	case ConsoleEncoding:
		encoderCfg.EncodeLevel = zapcore.CapitalColorLevelEncoder
		return zapcore.NewConsoleEncoder(encoderCfg)
	default:
		return zapcore.NewJSONEncoder(encoderCfg)
	}
}

func buildOptions(c Config, errW zapcore.WriteSyncer) []zap.Option {
	var opts []zap.Option
	if c.Caller {
		opts = append(opts, zap.AddCaller(), zap.AddCallerSkip(1))
	}
	if c.Stacktrace {
		opts = append(opts, zap.AddStacktrace(ErrorLevel))
	}
	opts = append(opts, zap.ErrorOutput(errW))
	return opts
}

func openOutput(output string, rotation RotationConfig) (zapcore.WriteSyncer, io.Closer, error) {
	switch output {
	case "stdout":
		return os.Stdout, nil, nil
	case "stderr":
		return os.Stderr, nil, nil
	default:
		if dir := filepath.Dir(output); dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, nil, fmt.Errorf("failed to create log directory %q: %w", dir, err)
			}
		}
		lj := &lumberjack.Logger{
			Filename:   output,
			MaxSize:    rotation.MaxSize,
			MaxBackups: rotation.MaxBackups,
			MaxAge:     rotation.MaxAge,
			Compress:   rotation.Compress,
			LocalTime:  rotation.LocalTime,
		}
		return zapcore.AddSync(lj), lj, nil
	}
}
