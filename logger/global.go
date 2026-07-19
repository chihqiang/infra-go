package logger

import (
	"context"
	"sync"
)

var (
	// global 全局 Logger 实例。
	global     ILogger
	globalLock sync.Mutex
)

func init() {
	// 初始化全局 Logger，默认输出到 stderr。
	global = New(Config{
		Level:       InfoLevel,
		Encoding:    JSONEncoding,
		Output:      []string{"stderr"},
		ErrorOutput: "stderr",
		Caller:      true,
	})
}

// SetGlobal 设置全局 Logger。
func SetGlobal(l ILogger) {
	globalLock.Lock()
	global = l
	globalLock.Unlock()
}

// GetGlobal 返回全局 Logger。
func GetGlobal() ILogger {
	globalLock.Lock()
	defer globalLock.Unlock()
	return global
}

// ReplaceGlobal 创建一个新 Logger 替换全局 Logger，返回旧的 Logger。
func ReplaceGlobal(cfg Config) ILogger {
	newLogger := New(cfg)
	globalLock.Lock()
	old := global
	global = newLogger
	globalLock.Unlock()
	return old
}

// --- 结构化日志 ---

func Debug(msg string, fields ...Field) { GetGlobal().Debug(msg, fields...) }
func Info(msg string, fields ...Field)  { GetGlobal().Info(msg, fields...) }
func Warn(msg string, fields ...Field)  { GetGlobal().Warn(msg, fields...) }
func Error(msg string, fields ...Field) { GetGlobal().Error(msg, fields...) }
func Panic(msg string, fields ...Field) { GetGlobal().Panic(msg, fields...) }
func Fatal(msg string, fields ...Field) { GetGlobal().Fatal(msg, fields...) }

// --- 格式化日志 ---

func Debugf(format string, args ...any) { GetGlobal().Debugf(format, args...) }
func Infof(format string, args ...any)  { GetGlobal().Infof(format, args...) }
func Warnf(format string, args ...any)  { GetGlobal().Warnf(format, args...) }
func Errorf(format string, args ...any) { GetGlobal().Errorf(format, args...) }
func Panicf(format string, args ...any) { GetGlobal().Panicf(format, args...) }
func Fatalf(format string, args ...any) { GetGlobal().Fatalf(format, args...) }

// --- 带上下文的结构化日志 ---

func DebugCtx(ctx context.Context, msg string, fields ...Field) {
	GetGlobal().DebugCtx(ctx, msg, fields...)
}
func InfoCtx(ctx context.Context, msg string, fields ...Field) {
	GetGlobal().InfoCtx(ctx, msg, fields...)
}
func WarnCtx(ctx context.Context, msg string, fields ...Field) {
	GetGlobal().WarnCtx(ctx, msg, fields...)
}
func ErrorCtx(ctx context.Context, msg string, fields ...Field) {
	GetGlobal().ErrorCtx(ctx, msg, fields...)
}
func PanicCtx(ctx context.Context, msg string, fields ...Field) {
	GetGlobal().PanicCtx(ctx, msg, fields...)
}
func FatalCtx(ctx context.Context, msg string, fields ...Field) {
	GetGlobal().FatalCtx(ctx, msg, fields...)
}

// --- 带上下文的格式化日志 ---

func DebugfCtx(ctx context.Context, format string, args ...any) {
	GetGlobal().DebugfCtx(ctx, format, args...)
}
func InfofCtx(ctx context.Context, format string, args ...any) {
	GetGlobal().InfofCtx(ctx, format, args...)
}
func WarnfCtx(ctx context.Context, format string, args ...any) {
	GetGlobal().WarnfCtx(ctx, format, args...)
}
func ErrorfCtx(ctx context.Context, format string, args ...any) {
	GetGlobal().ErrorfCtx(ctx, format, args...)
}
func PanicfCtx(ctx context.Context, format string, args ...any) {
	GetGlobal().PanicfCtx(ctx, format, args...)
}
func FatalfCtx(ctx context.Context, format string, args ...any) {
	GetGlobal().FatalfCtx(ctx, format, args...)
}

// --- Sync ---

// Sync 刷新全局 Logger 的缓冲区。
func Sync() error {
	return GetGlobal().Sync()
}
