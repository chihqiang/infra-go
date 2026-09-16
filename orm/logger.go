package orm

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/chihqiang/infra-go/logger"
	gormlogger "gorm.io/gorm/logger"
)

// buildGormLogger builds the GORM logger.
// When a global logger is set, GORM logs are bridged into the logger package; otherwise the
// standard library log is used and writes to stdout.
func buildGormLogger(c Config) gormlogger.Interface {
	logLevel := gormlogger.LogLevel(c.LogLevel)

	var writer gormlogger.Writer
	if l := logger.GetGlobal(); l != nil {
		writer = newGormWriter(l)
	} else {
		// Fall back to standard output when no global logger is set
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

// gormWriter bridges GORM's log output into the logger package.
type gormWriter struct {
	log logger.ILogger
}

// newGormWriter creates a GORM log writer that uses logger.
func newGormWriter(l logger.ILogger) *gormWriter {
	return &gormWriter{log: l}
}

// Printf implements the gormlogger.Writer interface.
// It inspects the level marker in the GORM log message and forwards it to the matching
// logger method.
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
