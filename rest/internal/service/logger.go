package service

import "github.com/chihqiang/ten-go/core/logger"

// LoggerSync flushes a server-owned logger when the ServiceGroup stops. The
// logger remains installed globally after the server stops.
type LoggerSync struct {
	logger logger.ILogger
}

// NewLoggerSync creates a logger sync service.
func NewLoggerSync(log logger.ILogger) *LoggerSync {
	return &LoggerSync{logger: log}
}

// Start is a no-op.
func (*LoggerSync) Start() {}

// Stop flushes the logger.
func (s *LoggerSync) Stop() {
	if s.logger != nil {
		_ = s.logger.Sync()
	}
}
