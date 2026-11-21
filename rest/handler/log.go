package handler

import (
	"net/http"
	"time"

	"github.com/chihqiang/ten-go/core/logger"
	"github.com/chihqiang/ten-go/rest/internal/pathmatch"
	"github.com/chihqiang/ten-go/rest/internal/response"
)

// LogConfig configures LogHandler.
type LogConfig struct {
	Enabled     bool     `json:",default=true"`
	ServiceName string   `json:",optional"`
	SkipPaths   []string `json:",optional"`
}

// LogHandler writes one access-log line per request.
type LogHandler struct {
	config    LogConfig
	log       logger.ILogger
	skipPaths *pathmatch.Matcher
}

// NewLogHandler creates a LogHandler. A nil log resolves the global logger on
// every request, so a later logger replacement still takes effect.
func NewLogHandler(config LogConfig, log logger.ILogger) *LogHandler {
	return &LogHandler{
		config:    config,
		log:       log,
		skipPaths: pathmatch.New(config.SkipPaths),
	}
}

// Handle wraps next with access logging.
func (h *LogHandler) Handle(next http.HandlerFunc) http.HandlerFunc {
	if !h.config.Enabled {
		return next
	}

	return func(w http.ResponseWriter, r *http.Request) {
		if h.skipPaths.Match(r.URL.Path) {
			next(w, r)
			return
		}

		start := time.Now()
		recorder := response.NewStatusWriter(w)
		completed := false
		// Log from a defer so a panicking handler still leaves an access-log
		// line before the stack unwinds to RecoveryHandler (which will answer
		// with 500).
		defer func() {
			status := recorder.Status()
			if !completed {
				status = http.StatusInternalServerError
			}

			log := h.log
			if log == nil {
				log = logger.GetGlobal()
			}

			fields := []logger.Field{
				logger.String("method", r.Method),
				logger.String("path", r.URL.Path),
				logger.Int("status", status),
				logger.Duration("duration", time.Since(start)),
				logger.String("ip", clientIP(r)),
			}
			if h.config.ServiceName != "" {
				fields = append(fields, logger.String("service", h.config.ServiceName))
			}
			if pattern := PatternFromContext(r.Context()); pattern != "" {
				fields = append(fields, logger.String("pattern", pattern))
			}

			message := "http request"
			switch {
			case status >= http.StatusInternalServerError:
				log.ErrorCtx(r.Context(), message, fields...)
			case status >= http.StatusBadRequest:
				log.WarnCtx(r.Context(), message, fields...)
			default:
				log.InfoCtx(r.Context(), message, fields...)
			}
		}()

		next(recorder, r)
		completed = true
	}
}
