package logger

import (
	"context"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Field 日志字段，用于结构化日志的键值对传递。
// 本类型是对底层实现的类型别名，用户无需关心其内部结构。
type Field = zap.Field

// --- 字段构造函数 ---

// String 创建一个 string 类型的日志字段。
func String(key, val string) Field { return zap.String(key, val) }

// Int 创建一个 int 类型的日志字段。
func Int(key string, val int) Field { return zap.Int(key, val) }

// Int64 创建一个 int64 类型的日志字段。
func Int64(key string, val int64) Field { return zap.Int64(key, val) }

// Float64 创建一个 float64 类型的日志字段。
func Float64(key string, val float64) Field { return zap.Float64(key, val) }

// Bool 创建一个 bool 类型的日志字段。
func Bool(key string, val bool) Field { return zap.Bool(key, val) }

// Duration 创建一个 time.Duration 类型的日志字段。
func Duration(key string, val time.Duration) Field { return zap.Duration(key, val) }

// Time 创建一个 time.Time 类型的日志字段。
func Time(key string, val time.Time) Field { return zap.Time(key, val) }

// Any 创建一个任意类型的日志字段。
func Any(key string, val any) Field { return zap.Any(key, val) }

// Err 创建一个 error 类型的日志字段，固定键名为 "error"。
func Err(err error) Field { return zap.Error(err) }

// Level 日志级别类型。
type Level = zapcore.Level

// 支持的日志级别。
const (
	DebugLevel  Level = zapcore.DebugLevel
	InfoLevel   Level = zapcore.InfoLevel
	WarnLevel   Level = zapcore.WarnLevel
	ErrorLevel  Level = zapcore.ErrorLevel
	DPanicLevel Level = zapcore.DPanicLevel
	PanicLevel  Level = zapcore.PanicLevel
	FatalLevel  Level = zapcore.FatalLevel
)

// Logger 定义日志记录器核心接口。
// 所有实现必须提供基本的日志方法与级别检查能力。
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
