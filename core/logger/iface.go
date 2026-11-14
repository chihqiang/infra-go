package logger

import (
	"context"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Field is a log field, used to pass key-value pairs into structured logging.
// The type is an alias of the underlying implementation, so users need not care about
// its internals.
type Field = zap.Field

// --- Field constructors ---

// String creates a log field of type string.
func String(key, val string) Field { return zap.String(key, val) }

// Int creates a log field of type int.
func Int(key string, val int) Field { return zap.Int(key, val) }

// Int64 creates a log field of type int64.
func Int64(key string, val int64) Field { return zap.Int64(key, val) }

// Float64 creates a log field of type float64.
func Float64(key string, val float64) Field { return zap.Float64(key, val) }

// Bool creates a log field of type bool.
func Bool(key string, val bool) Field { return zap.Bool(key, val) }

// Duration creates a log field of type time.Duration.
func Duration(key string, val time.Duration) Field { return zap.Duration(key, val) }

// Time creates a log field of type time.Time.
func Time(key string, val time.Time) Field { return zap.Time(key, val) }

// Any creates a log field holding a value of any type.
func Any(key string, val any) Field { return zap.Any(key, val) }

// Err creates a log field of type error, with the fixed key name "error".
func Err(err error) Field { return zap.Error(err) }

// Level is the log level type.
type Level = zapcore.Level

// Supported log levels.
const (
	DebugLevel  Level = zapcore.DebugLevel
	InfoLevel   Level = zapcore.InfoLevel
	WarnLevel   Level = zapcore.WarnLevel
	ErrorLevel  Level = zapcore.ErrorLevel
	DPanicLevel Level = zapcore.DPanicLevel
	PanicLevel  Level = zapcore.PanicLevel
	FatalLevel  Level = zapcore.FatalLevel
)

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

	Sync() error
}
