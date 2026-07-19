package logger

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/chihqiang/infra-go/mapping"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

// Encoding 日志编码格式类型。
type Encoding string

const (
	// JSONEncoding JSON 格式输出。
	JSONEncoding Encoding = "json"
	// ConsoleEncoding 控制台格式输出，人类可读。
	ConsoleEncoding Encoding = "console"
)

// ContextExtractor 从 context 中提取日志字段的函数。
type ContextExtractor func(ctx context.Context) []Field

var (
	contextExtractors []ContextExtractor
	extractorsMu      sync.RWMutex
)

// RegisterContextExtractor 注册一个上下文字段提取器。
func RegisterContextExtractor(extractor ContextExtractor) {
	extractorsMu.Lock()
	contextExtractors = append(contextExtractors, extractor)
	extractorsMu.Unlock()
}

func extractContextFields(ctx context.Context) []Field {
	extractorsMu.RLock()
	extractors := contextExtractors
	extractorsMu.RUnlock()

	var fields []Field
	for _, extractor := range extractors {
		fields = append(fields, extractor(ctx)...)
	}
	return fields
}

// RotationConfig 日志文件轮转配置。
// 基于 lumberjack 实现，支持按文件大小切割、按天数保留。
type RotationConfig struct {
	// MaxSize 单个日志文件的最大大小（MB），默认 100MB。
	MaxSize int `json:",default=100"`
	// MaxBackups 保留的旧日志文件最大数量，默认 7。
	MaxBackups int `json:",default=7"`
	// MaxAge 保留旧日志文件的最大天数，默认 30。
	MaxAge int `json:",default=30"`
	// Compress 是否压缩旧日志文件（gzip），默认 false。
	Compress bool `json:",optional"`
	// LocalTime 是否使用本地时间命名备份文件，默认 true。
	LocalTime bool `json:",default=true"`
}

// Config 日志配置。
type Config struct {
	// Level 日志级别，默认 InfoLevel（zapcore.InfoLevel = 0）。
	Level Level `json:",default=0"`
	// Encoding 编码格式，默认 JSONEncoding。
	Encoding Encoding `json:",default=json"`
	// Output 输出目标，支持 "stdout"、"stderr" 或文件路径，默认 ["stdout"]。
	Output []string `json:",default=[stdout]"`
	// ErrorOutput 错误输出目标，默认 "stderr"。
	ErrorOutput string `json:",default=stderr"`
	// Caller 是否记录调用者信息（文件名和行号），默认 true。
	Caller bool `json:",default=true"`
	// Stacktrace 是否在 Error 及以上级别记录堆栈，默认 false。
	Stacktrace bool `json:",optional"`
	// TimeLayout 时间格式布局，默认 ISO8601。
	TimeLayout string `json:",default=2006-01-02T15:04:05.000Z07:00"`
	// AppName 应用名称，会作为固定字段输出，默认空。
	AppName string `json:",optional"`
	// Rotation 日志文件轮转配置。
	Rotation RotationConfig
}

// fillDefaultUnmarshaler 用于填充默认值的反序列化器。
var fillDefaultUnmarshaler = mapping.NewUnmarshaler("json", mapping.WithDefault())

// Logger 日志记录器，实现 LoggerInterface。
type Logger struct {
	zap     *zap.Logger
	sugar   *zap.SugaredLogger
	config  Config
	closers []io.Closer
}

// New 创建一个新的 Logger。
func New(cfg Config) ILogger {
	c := fillDefault(cfg)

	encoder := newEncoder(c)
	levelEnabler := zap.LevelEnablerFunc(func(lvl Level) bool {
		return lvl >= c.Level
	})

	// 构建输出写入器
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

	// 构建错误输出写入器
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

// Default 返回一个使用默认配置的 Logger。
func Default() ILogger {
	return New(Config{})
}

// --- 结构化日志方法 ---

func (l *Logger) Debug(msg string, fields ...Field) { l.zap.Debug(msg, fields...) }
func (l *Logger) Info(msg string, fields ...Field)  { l.zap.Info(msg, fields...) }
func (l *Logger) Warn(msg string, fields ...Field)  { l.zap.Warn(msg, fields...) }
func (l *Logger) Error(msg string, fields ...Field) { l.zap.Error(msg, fields...) }
func (l *Logger) Panic(msg string, fields ...Field) { l.zap.Panic(msg, fields...) }
func (l *Logger) Fatal(msg string, fields ...Field) { l.zap.Fatal(msg, fields...) }

// --- 格式化日志方法 ---

func (l *Logger) Debugf(format string, args ...any) { l.zap.Debug(fmt.Sprintf(format, args...)) }
func (l *Logger) Infof(format string, args ...any)  { l.zap.Info(fmt.Sprintf(format, args...)) }
func (l *Logger) Warnf(format string, args ...any)  { l.zap.Warn(fmt.Sprintf(format, args...)) }
func (l *Logger) Errorf(format string, args ...any) { l.zap.Error(fmt.Sprintf(format, args...)) }
func (l *Logger) Panicf(format string, args ...any) { l.zap.Panic(fmt.Sprintf(format, args...)) }
func (l *Logger) Fatalf(format string, args ...any) { l.zap.Fatal(fmt.Sprintf(format, args...)) }

// --- 带上下文的结构化日志方法 ---

func (l *Logger) DebugCtx(ctx context.Context, msg string, fields ...Field) {
	l.zap.With(extractContextFields(ctx)...).Debug(msg, fields...)
}
func (l *Logger) InfoCtx(ctx context.Context, msg string, fields ...Field) {
	l.zap.With(extractContextFields(ctx)...).Info(msg, fields...)
}
func (l *Logger) WarnCtx(ctx context.Context, msg string, fields ...Field) {
	l.zap.With(extractContextFields(ctx)...).Warn(msg, fields...)
}
func (l *Logger) ErrorCtx(ctx context.Context, msg string, fields ...Field) {
	l.zap.With(extractContextFields(ctx)...).Error(msg, fields...)
}
func (l *Logger) PanicCtx(ctx context.Context, msg string, fields ...Field) {
	l.zap.With(extractContextFields(ctx)...).Panic(msg, fields...)
}
func (l *Logger) FatalCtx(ctx context.Context, msg string, fields ...Field) {
	l.zap.With(extractContextFields(ctx)...).Fatal(msg, fields...)
}

// --- 带上下文的格式化日志方法 ---

func (l *Logger) DebugfCtx(ctx context.Context, format string, args ...any) {
	l.zap.With(extractContextFields(ctx)...).Debug(fmt.Sprintf(format, args...))
}
func (l *Logger) InfofCtx(ctx context.Context, format string, args ...any) {
	l.zap.With(extractContextFields(ctx)...).Info(fmt.Sprintf(format, args...))
}
func (l *Logger) WarnfCtx(ctx context.Context, format string, args ...any) {
	l.zap.With(extractContextFields(ctx)...).Warn(fmt.Sprintf(format, args...))
}
func (l *Logger) ErrorfCtx(ctx context.Context, format string, args ...any) {
	l.zap.With(extractContextFields(ctx)...).Error(fmt.Sprintf(format, args...))
}
func (l *Logger) PanicfCtx(ctx context.Context, format string, args ...any) {
	l.zap.With(extractContextFields(ctx)...).Panic(fmt.Sprintf(format, args...))
}
func (l *Logger) FatalfCtx(ctx context.Context, format string, args ...any) {
	l.zap.With(extractContextFields(ctx)...).Fatal(fmt.Sprintf(format, args...))
}

// --- Sync ---

// Sync 刷新缓冲区中的日志。
func (l *Logger) Sync() error {
	return l.zap.Sync()
}

// --- 额外方法（不在接口中） ---

// Close 关闭日志记录器，刷新缓冲区并关闭文件输出。
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

// --- 内部函数 ---

func fillDefault(cfg Config) Config {
	var c Config
	if err := fillDefaultUnmarshaler.Unmarshal(map[string]any{}, &c); err != nil {
		panic(fmt.Errorf("logger: failed to fill default config: %w", err))
	}

	if cfg.Level != 0 {
		c.Level = cfg.Level
	}
	if cfg.Encoding != "" {
		c.Encoding = cfg.Encoding
	}
	if len(cfg.Output) > 0 {
		c.Output = cfg.Output
	}
	if cfg.ErrorOutput != "" {
		c.ErrorOutput = cfg.ErrorOutput
	}
	if cfg.TimeLayout != "" {
		c.TimeLayout = cfg.TimeLayout
	}
	c.AppName = cfg.AppName
	if cfg.Caller {
		c.Caller = cfg.Caller
	}
	if cfg.Stacktrace {
		c.Stacktrace = cfg.Stacktrace
	}
	if cfg.Rotation.MaxSize > 0 {
		c.Rotation.MaxSize = cfg.Rotation.MaxSize
	}
	if cfg.Rotation.MaxBackups > 0 {
		c.Rotation.MaxBackups = cfg.Rotation.MaxBackups
	}
	if cfg.Rotation.MaxAge > 0 {
		c.Rotation.MaxAge = cfg.Rotation.MaxAge
	}
	if cfg.Rotation.Compress {
		c.Rotation.Compress = cfg.Rotation.Compress
	}
	if cfg.Rotation.LocalTime {
		c.Rotation.LocalTime = cfg.Rotation.LocalTime
	}

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
