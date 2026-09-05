// Package logger 提供基于 zap + lumberjack 的结构化日志封装。
//
// 支持结构化/格式化/带 Context 三种日志风格、JSON 与控制台两种编码、
// 文件按大小轮转与保留策略；提供包级全局 Logger，可用 SetGlobal / ReplaceGlobal 切换。
package logger

import (
	"context"
	"sync/atomic"
)

// 全局 Logger：所有包级日志函数（Info/Error 等）均转发到 GetGlobal() 返回的实例。
// 默认输出到 stderr（JSON 格式，Info 级别），可用 SetGlobal / ReplaceGlobal 替换。
var (
	// global 全局 Logger 实例。
	global atomic.Pointer[ILogger]
)

func init() {
	// 初始化全局 Logger，默认输出到 stderr。
	l := ILogger(New(Config{
		Level:       InfoLevel,
		Encoding:    JSONEncoding,
		Output:      []string{"stderr"},
		ErrorOutput: "stderr",
		Caller:      true,
	}))
	global.Store(&l)
}

// SetGlobal 设置全局 Logger，之后所有包级日志函数都写入 l。
func SetGlobal(l ILogger) {
	global.Store(&l)
}

// GetGlobal 返回当前的全局 Logger。
func GetGlobal() ILogger {
	return *global.Load()
}

// ReplaceGlobal 用默认配置创建新 Logger 替换全局实例，返回被替换的旧 Logger。
// 便于在运行时热切换日志配置（如重载配置文件）后手动关闭旧实例。
func ReplaceGlobal(cfg Config) ILogger {
	newLogger := New(cfg)
	old := global.Swap(&newLogger)
	return *old
}

// --- 结构化日志 ---

// Debug 以 Debug 级别记录一条结构化日志到全局 Logger。
func Debug(msg string, fields ...Field) { GetGlobal().Debug(msg, fields...) }

// Info 以 Info 级别记录一条结构化日志到全局 Logger。
func Info(msg string, fields ...Field) { GetGlobal().Info(msg, fields...) }

// Warn 以 Warn 级别记录一条结构化日志到全局 Logger。
func Warn(msg string, fields ...Field) { GetGlobal().Warn(msg, fields...) }

// Error 以 Error 级别记录一条结构化日志到全局 Logger。
func Error(msg string, fields ...Field) { GetGlobal().Error(msg, fields...) }

// Panic 以 Panic 级别记录日志后触发 panic。
func Panic(msg string, fields ...Field) { GetGlobal().Panic(msg, fields...) }

// Fatal 以 Fatal 级别记录日志后调用 os.Exit(1)。
func Fatal(msg string, fields ...Field) { GetGlobal().Fatal(msg, fields...) }

// --- 格式化日志 ---

// Debugf 以 Debug 级别记录格式化日志（fmt.Sprintf 风格）。
func Debugf(format string, args ...any) { GetGlobal().Debugf(format, args...) }

// Infof 以 Info 级别记录格式化日志。
func Infof(format string, args ...any) { GetGlobal().Infof(format, args...) }

// Warnf 以 Warn 级别记录格式化日志。
func Warnf(format string, args ...any) { GetGlobal().Warnf(format, args...) }

// Errorf 以 Error 级别记录格式化日志。
func Errorf(format string, args ...any) { GetGlobal().Errorf(format, args...) }

// Panicf 以 Panic 级别记录格式化日志后触发 panic。
func Panicf(format string, args ...any) { GetGlobal().Panicf(format, args...) }

// Fatalf 以 Fatal 级别记录格式化日志后调用 os.Exit(1)。
func Fatalf(format string, args ...any) { GetGlobal().Fatalf(format, args...) }

// --- 带上下文的结构化日志 ---

// DebugCtx 以 Debug 级别记录日志，并自动并入 ctx 中注册的上下文提取器字段。
func DebugCtx(ctx context.Context, msg string, fields ...Field) {
	GetGlobal().DebugCtx(ctx, msg, fields...)
}

// InfoCtx 以 Info 级别记录日志，并自动并入 ctx 中注册的上下文提取器字段。
func InfoCtx(ctx context.Context, msg string, fields ...Field) {
	GetGlobal().InfoCtx(ctx, msg, fields...)
}

// WarnCtx 以 Warn 级别记录日志，并自动并入 ctx 中注册的上下文提取器字段。
func WarnCtx(ctx context.Context, msg string, fields ...Field) {
	GetGlobal().WarnCtx(ctx, msg, fields...)
}

// ErrorCtx 以 Error 级别记录日志，并自动并入 ctx 中注册的上下文提取器字段。
func ErrorCtx(ctx context.Context, msg string, fields ...Field) {
	GetGlobal().ErrorCtx(ctx, msg, fields...)
}

// PanicCtx 以 Panic 级别记录日志后触发 panic，并自动并入上下文提取器字段。
func PanicCtx(ctx context.Context, msg string, fields ...Field) {
	GetGlobal().PanicCtx(ctx, msg, fields...)
}

// FatalCtx 以 Fatal 级别记录日志后调用 os.Exit(1)，并自动并入上下文提取器字段。
func FatalCtx(ctx context.Context, msg string, fields ...Field) {
	GetGlobal().FatalCtx(ctx, msg, fields...)
}

// --- 带上下文的格式化日志 ---

// DebugfCtx 以 Debug 级别记录格式化日志，并自动并入上下文提取器字段。
func DebugfCtx(ctx context.Context, format string, args ...any) {
	GetGlobal().DebugfCtx(ctx, format, args...)
}

// InfofCtx 以 Info 级别记录格式化日志，并自动并入上下文提取器字段。
func InfofCtx(ctx context.Context, format string, args ...any) {
	GetGlobal().InfofCtx(ctx, format, args...)
}

// WarnfCtx 以 Warn 级别记录格式化日志，并自动并入上下文提取器字段。
func WarnfCtx(ctx context.Context, format string, args ...any) {
	GetGlobal().WarnfCtx(ctx, format, args...)
}

// ErrorfCtx 以 Error 级别记录格式化日志，并自动并入上下文提取器字段。
func ErrorfCtx(ctx context.Context, format string, args ...any) {
	GetGlobal().ErrorfCtx(ctx, format, args...)
}

// PanicfCtx 以 Panic 级别记录格式化日志后触发 panic，并自动并入上下文提取器字段。
func PanicfCtx(ctx context.Context, format string, args ...any) {
	GetGlobal().PanicfCtx(ctx, format, args...)
}

// FatalfCtx 以 Fatal 级别记录格式化日志后调用 os.Exit(1)，并自动并入上下文提取器字段。
func FatalfCtx(ctx context.Context, format string, args ...any) {
	GetGlobal().FatalfCtx(ctx, format, args...)
}

// --- Sync ---

// Sync 刷新全局 Logger 的缓冲区。
func Sync() error {
	return GetGlobal().Sync()
}
