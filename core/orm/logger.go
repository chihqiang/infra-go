package orm

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/chihqiang/infra-go/core/logger"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// gormLogger bridges GORM logs into the logger package.
//
// It implements gorm's logger.Interface directly instead of writing through a
// gormlogger.Writer: the Writer interface only receives a formatted string, and
// the old implementation guessed the level with strings.Contains(msg, "error").
// A real SQL error such as "no such table: users" does not contain that word, so
// it was logged at Info and disappeared when the application ran at warn level;
// conversely, normal SQL mentioning a table like "error_logs" was logged as an
// error with a stack trace.
type gormLogger struct {
	log           logger.ILogger
	level         gormlogger.LogLevel
	slowThreshold time.Duration
}

// buildGormLogger builds the GORM logger. When a global logger is set, GORM logs are
// bridged into the logger package; otherwise the standard library log is used and
// writes to stdout.
func buildGormLogger(c Config) gormlogger.Interface {
	l := logger.GetGlobal()
	if l == nil {
		// GetGlobal is initialised by the logger package's init, so this is only a
		// safety net for an explicitly cleared global.
		l = logger.Default()
	}
	return &gormLogger{
		log:           l,
		level:         gormlogger.LogLevel(c.LogLevel),
		slowThreshold: c.SlowThreshold,
	}
}

// LogMode implements gormlogger.Interface; it returns a copy with the new level.
func (l *gormLogger) LogMode(level gormlogger.LogLevel) gormlogger.Interface {
	clone := *l
	clone.level = level
	return &clone
}

// Info logs a non-SQL informational message from gorm.
func (l *gormLogger) Info(ctx context.Context, msg string, args ...any) {
	if l.level < gormlogger.Info {
		return
	}
	l.log.InfoCtx(ctx, "gorm: "+fmt.Sprintf(msg, args...))
}

// Warn logs a non-SQL warning from gorm.
func (l *gormLogger) Warn(ctx context.Context, msg string, args ...any) {
	if l.level < gormlogger.Warn {
		return
	}
	l.log.WarnCtx(ctx, "gorm: "+fmt.Sprintf(msg, args...))
}

// Error logs a non-SQL error from gorm.
func (l *gormLogger) Error(ctx context.Context, msg string, args ...any) {
	if l.level < gormlogger.Error {
		return
	}
	l.log.ErrorCtx(ctx, "gorm: "+fmt.Sprintf(msg, args...))
}

// Trace logs one SQL statement. The severity is decided from the real error and
// the measured duration, never from the text of the statement.
func (l *gormLogger) Trace(ctx context.Context, begin time.Time, fc func() (sql string, rowsAffected int64), err error) {
	if l.level <= gormlogger.Silent {
		return
	}

	elapsed := time.Since(begin)
	sql, rows := fc()
	fields := []logger.Field{
		logger.String("sql", sql),
		logger.Int64("rows", rows),
		logger.Duration("duration", elapsed),
	}

	switch {
	case err != nil && !errors.Is(err, gorm.ErrRecordNotFound):
		if l.level < gormlogger.Error {
			return
		}
		l.log.ErrorCtx(ctx, "gorm: query failed", append(fields, logger.Err(err))...)
	case l.slowThreshold > 0 && elapsed > l.slowThreshold:
		if l.level < gormlogger.Warn {
			return
		}
		l.log.WarnCtx(ctx, "gorm: slow query", fields...)
	case l.level >= gormlogger.Info:
		l.log.InfoCtx(ctx, "gorm: query", fields...)
	}
}
