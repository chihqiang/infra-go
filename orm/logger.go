package orm

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/chihqiang/infra-go/logger"
	gormlogger "gorm.io/gorm/logger"
)

// buildGormLogger 构建 GORM 日志记录器。
// 如果已设置全局 logger，则将 GORM 日志桥接到 logger 包；
// 否则使用标准库 log 输出到 stdout。
func buildGormLogger(c Config) gormlogger.Interface {
	logLevel := gormlogger.LogLevel(c.LogLevel)

	var writer gormlogger.Writer
	if l := logger.GetGlobal(); l != nil {
		writer = newGormWriter(l)
	} else {
		// 未设置全局 logger 时使用标准输出
		writer = log.New(os.Stdout, "\r\n", log.LstdFlags)
	}

	return gormlogger.New(
		writer,
		gormlogger.Config{
			SlowThreshold:             c.SlowThreshold,
			LogLevel:                  logLevel,
			IgnoreRecordNotFoundError: true,
			Colorful:                  c.Colorful,
		},
	)
}

// gormWriter 将 GORM 的日志输出桥接到 logger 包。
type gormWriter struct {
	log logger.ILogger
}

// newGormWriter 创建一个使用 logger 的 GORM 日志写入器。
func newGormWriter(l logger.ILogger) *gormWriter {
	return &gormWriter{log: l}
}

// Printf 实现 gormlogger.Writer 接口。
// 根据 GORM 日志内容中的级别标识，转发到对应级别的 logger 方法。
func (w *gormWriter) Printf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	lower := strings.ToLower(msg)

	switch {
	case strings.Contains(lower, "error"):
		w.log.Error(msg)
	case strings.Contains(lower, "warn"), strings.Contains(lower, "slow"):
		w.log.Warn(msg)
	default:
		w.log.Info(msg)
	}
}
