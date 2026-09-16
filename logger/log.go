// Package logger provides a structured logging wrapper built on zap + lumberjack.
//
// It supports three logging styles (structured / formatted / context-aware), two encodings
// (JSON and console) and size-based rotation with a retention policy for log files. A
// package-level global Logger is provided and can be swapped with SetGlobal / ReplaceGlobal.
package logger

import (
	"context"
	"sync/atomic"
)

// Global Logger: all package-level log functions (Info/Error, ...) forward to the instance
// returned by GetGlobal(). It writes to stderr by default (JSON encoding, Info level) and can
// be replaced via SetGlobal / ReplaceGlobal.
var (
	// global is the global Logger instance.
	global atomic.Pointer[ILogger]
)

func init() {
	// Initialise the global Logger, which writes to stderr by default.
	l := ILogger(New(Config{
		Level:       InfoLevel,
		Encoding:    JSONEncoding,
		Output:      []string{"stderr"},
		ErrorOutput: "stderr",
		Caller:      true,
	}))
	global.Store(&l)
}

// SetGlobal sets the global Logger; all package-level log functions write to l afterwards.
func SetGlobal(l ILogger) {
	global.Store(&l)
}

// GetGlobal returns the current global Logger.
func GetGlobal() ILogger {
	return *global.Load()
}

// ReplaceGlobal builds a new Logger from cfg, swaps it in as the global instance and returns
// the replaced (old) Logger.
// This makes it easy to hot-swap the log configuration at runtime (for example after
// reloading a config file) and close the old instance manually.
func ReplaceGlobal(cfg Config) ILogger {
	newLogger := New(cfg)
	old := global.Swap(&newLogger)
	return *old
}

// --- Structured logging ---

// Debug logs one structured entry at Debug level to the global Logger.
func Debug(msg string, fields ...Field) { GetGlobal().Debug(msg, fields...) }

// Info logs one structured entry at Info level to the global Logger.
func Info(msg string, fields ...Field) { GetGlobal().Info(msg, fields...) }

// Warn logs one structured entry at Warn level to the global Logger.
func Warn(msg string, fields ...Field) { GetGlobal().Warn(msg, fields...) }

// Error logs one structured entry at Error level to the global Logger.
func Error(msg string, fields ...Field) { GetGlobal().Error(msg, fields...) }

// Panic logs at Panic level and then panics.
func Panic(msg string, fields ...Field) { GetGlobal().Panic(msg, fields...) }

// Fatal logs at Fatal level and then calls os.Exit(1).
func Fatal(msg string, fields ...Field) { GetGlobal().Fatal(msg, fields...) }

// --- Formatted logging ---

// Debugf logs a formatted entry at Debug level (fmt.Sprintf style).
func Debugf(format string, args ...any) { GetGlobal().Debugf(format, args...) }

// Infof logs a formatted entry at Info level.
func Infof(format string, args ...any) { GetGlobal().Infof(format, args...) }

// Warnf logs a formatted entry at Warn level.
func Warnf(format string, args ...any) { GetGlobal().Warnf(format, args...) }

// Errorf logs a formatted entry at Error level.
func Errorf(format string, args ...any) { GetGlobal().Errorf(format, args...) }

// Panicf logs a formatted entry at Panic level and then panics.
func Panicf(format string, args ...any) { GetGlobal().Panicf(format, args...) }

// Fatalf logs a formatted entry at Fatal level and then calls os.Exit(1).
func Fatalf(format string, args ...any) { GetGlobal().Fatalf(format, args...) }

// --- Structured logging with context ---

// DebugCtx logs at Debug level and merges in the context extractor fields registered in ctx.
func DebugCtx(ctx context.Context, msg string, fields ...Field) {
	GetGlobal().DebugCtx(ctx, msg, fields...)
}

// InfoCtx logs at Info level and merges in the context extractor fields registered in ctx.
func InfoCtx(ctx context.Context, msg string, fields ...Field) {
	GetGlobal().InfoCtx(ctx, msg, fields...)
}

// WarnCtx logs at Warn level and merges in the context extractor fields registered in ctx.
func WarnCtx(ctx context.Context, msg string, fields ...Field) {
	GetGlobal().WarnCtx(ctx, msg, fields...)
}

// ErrorCtx logs at Error level and merges in the context extractor fields registered in ctx.
func ErrorCtx(ctx context.Context, msg string, fields ...Field) {
	GetGlobal().ErrorCtx(ctx, msg, fields...)
}

// PanicCtx logs at Panic level, then panics, merging in the context extractor fields.
func PanicCtx(ctx context.Context, msg string, fields ...Field) {
	GetGlobal().PanicCtx(ctx, msg, fields...)
}

// FatalCtx logs at Fatal level, then calls os.Exit(1), merging in the context extractor
// fields.
func FatalCtx(ctx context.Context, msg string, fields ...Field) {
	GetGlobal().FatalCtx(ctx, msg, fields...)
}

// --- Formatted logging with context ---

// DebugfCtx logs a formatted entry at Debug level and merges in the context extractor fields.
func DebugfCtx(ctx context.Context, format string, args ...any) {
	GetGlobal().DebugfCtx(ctx, format, args...)
}

// InfofCtx logs a formatted entry at Info level and merges in the context extractor fields.
func InfofCtx(ctx context.Context, format string, args ...any) {
	GetGlobal().InfofCtx(ctx, format, args...)
}

// WarnfCtx logs a formatted entry at Warn level and merges in the context extractor fields.
func WarnfCtx(ctx context.Context, format string, args ...any) {
	GetGlobal().WarnfCtx(ctx, format, args...)
}

// ErrorfCtx logs a formatted entry at Error level and merges in the context extractor fields.
func ErrorfCtx(ctx context.Context, format string, args ...any) {
	GetGlobal().ErrorfCtx(ctx, format, args...)
}

// PanicfCtx logs a formatted entry at Panic level, then panics, merging in the extractor
// fields.
func PanicfCtx(ctx context.Context, format string, args ...any) {
	GetGlobal().PanicfCtx(ctx, format, args...)
}

// FatalfCtx logs a formatted entry at Fatal level, then calls os.Exit(1), merging in the
// extractor fields.
func FatalfCtx(ctx context.Context, format string, args ...any) {
	GetGlobal().FatalfCtx(ctx, format, args...)
}

// --- Sync ---

// Sync flushes the buffers of the global Logger.
func Sync() error {
	return GetGlobal().Sync()
}
