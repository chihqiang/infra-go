package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/chihqiang/infra-go/core/logger"
	"github.com/chihqiang/infra-go/rest/chain"
	"github.com/chihqiang/infra-go/rest/internal/pathmatch"
	"github.com/chihqiang/infra-go/rest/internal/realip"
	"github.com/chihqiang/infra-go/rest/internal/response"
)

// LogOption customizes LogHandler.
type LogOption func(*logOptions)

type logOptions struct {
	skipPaths   []string
	serviceName string
}

// WithLogSkipPaths omits matching request paths from the access log. Rules
// accept exact paths, a trailing "*" prefix and glob wildcards.
func WithLogSkipPaths(paths ...string) LogOption {
	return func(opts *logOptions) {
		opts.skipPaths = append(opts.skipPaths, paths...)
	}
}

// WithLogServiceName adds a service field to every access-log line.
func WithLogServiceName(name string) LogOption {
	return func(opts *logOptions) {
		opts.serviceName = name
	}
}

// AccessLogger is the subset of core/logger.ILogger used by LogHandlerWith.
type AccessLogger interface {
	InfoCtx(ctx context.Context, msg string, fields ...logger.Field)
	WarnCtx(ctx context.Context, msg string, fields ...logger.Field)
	ErrorCtx(ctx context.Context, msg string, fields ...logger.Field)
}

// LogHandler writes one access-log line per request using the global logger,
// resolved per request so a later logger replacement still takes effect.
func LogHandler(opts ...LogOption) chain.Middleware {
	return LogHandlerWith(globalLogger{}, opts...)
}

type globalLogger struct{}

func (globalLogger) InfoCtx(ctx context.Context, msg string, fields ...logger.Field) {
	logger.GetGlobal().InfoCtx(ctx, msg, fields...)
}

func (globalLogger) WarnCtx(ctx context.Context, msg string, fields ...logger.Field) {
	logger.GetGlobal().WarnCtx(ctx, msg, fields...)
}

func (globalLogger) ErrorCtx(ctx context.Context, msg string, fields ...logger.Field) {
	logger.GetGlobal().ErrorCtx(ctx, msg, fields...)
}

// LogHandlerWith writes access logs through the given logger.
func LogHandlerWith(log AccessLogger, opts ...LogOption) chain.Middleware {
	options := logOptions{}
	for _, opt := range opts {
		opt(&options)
	}
	skipPaths := pathmatch.New(options.skipPaths)

	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if skipPaths.Match(r.URL.Path) {
				next(w, r)
				return
			}

			start := time.Now()
			recorder := response.NewStatusWriter(w)
			next(recorder, r)

			status := recorder.Status()
			fields := []logger.Field{
				logger.String("method", r.Method),
				logger.String("path", r.URL.Path),
				logger.Int("status", status),
				logger.Duration("duration", time.Since(start)),
				logger.String("ip", realip.FromRequest(r)),
			}
			if options.serviceName != "" {
				fields = append(fields, logger.String("service", options.serviceName))
			}
			if pattern := PatternFromContext(r.Context()); pattern != "" {
				fields = append(fields, logger.String("pattern", pattern))
			}
			if id, ok := RequestIDFromContext(r.Context()); ok {
				fields = append(fields, logger.String("request_id", id))
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
		}
	}
}
