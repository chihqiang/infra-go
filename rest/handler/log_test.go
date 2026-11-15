package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap/zapcore"

	"github.com/chihqiang/infra-go/core/logger"
)

type logEntry struct {
	level  string
	fields map[string]any
}

type recordingLogger struct {
	logger.ILogger
	entries []logEntry
}

func (r *recordingLogger) record(level string, fields []logger.Field) {
	values := make(map[string]any, len(fields))
	for _, field := range fields {
		switch field.Type {
		case zapcore.Int64Type, zapcore.DurationType:
			values[field.Key] = field.Integer
		default:
			values[field.Key] = field.String
		}
	}
	r.entries = append(r.entries, logEntry{level: level, fields: values})
}

func (r *recordingLogger) InfoCtx(_ context.Context, _ string, fields ...logger.Field) {
	r.record("info", fields)
}

func (r *recordingLogger) WarnCtx(_ context.Context, _ string, fields ...logger.Field) {
	r.record("warn", fields)
}

func (r *recordingLogger) ErrorCtx(_ context.Context, _ string, fields ...logger.Field) {
	r.record("error", fields)
}

func TestLogRecordsLevelFromStatus(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		wantLevel string
	}{
		{name: "ok", status: http.StatusOK, wantLevel: "info"},
		{name: "client error", status: http.StatusBadRequest, wantLevel: "warn"},
		{name: "server error", status: http.StatusInternalServerError, wantLevel: "error"},
	}

	for _, test := range tests {
		log := &recordingLogger{}
		handlerFn := LogHandlerWith(log)(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(test.status)
		})
		handlerFn.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/users", nil))

		if len(log.entries) != 1 {
			t.Fatalf("%s: entries = %d, want 1", test.name, len(log.entries))
		}
		entry := log.entries[0]
		if entry.level != test.wantLevel {
			t.Errorf("%s: level = %q, want %q", test.name, entry.level, test.wantLevel)
		}
		if entry.fields["status"] != int64(test.status) {
			t.Errorf("%s: status field = %v, want %d", test.name, entry.fields["status"], test.status)
		}
		if entry.fields["method"] != http.MethodGet || entry.fields["path"] != "/users" {
			t.Errorf("%s: request fields = %v", test.name, entry.fields)
		}
		if _, ok := entry.fields["duration"]; !ok {
			t.Errorf("%s: duration field missing", test.name)
		}
	}
}

func TestLogRecordsRequestIDFromContext(t *testing.T) {
	log := &recordingLogger{}
	logFn := LogHandlerWith(log)(func(http.ResponseWriter, *http.Request) {})

	request := httptest.NewRequest(http.MethodGet, "/users", nil)
	RequestIDHandler(func(w http.ResponseWriter, r *http.Request) {
		logFn.ServeHTTP(w, r)
	}).ServeHTTP(httptest.NewRecorder(), request)

	if len(log.entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(log.entries))
	}
	if _, ok := log.entries[0].fields["request_id"]; !ok {
		t.Fatal("request_id field missing from access log")
	}
}

func TestLogHandlerSkipsPathsAndAddsService(t *testing.T) {
	log := &recordingLogger{}
	logFn := LogHandlerWith(log, WithLogSkipPaths("/healthz"), WithLogServiceName("demo"))(
		func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) },
	)

	logFn.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if len(log.entries) != 0 {
		t.Fatalf("skipped path produced %d log entries", len(log.entries))
	}

	logFn.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/users", nil))
	if len(log.entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(log.entries))
	}
	if log.entries[0].fields["service"] != "demo" {
		t.Fatalf("service field = %v, want demo", log.entries[0].fields["service"])
	}
}

func TestLogHandlerSkipsWildcardPaths(t *testing.T) {
	log := &recordingLogger{}
	logFn := LogHandlerWith(log, WithLogSkipPaths("/health*"))(
		func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) },
	)

	logFn.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/health/live", nil))
	if len(log.entries) != 0 {
		t.Fatalf("wildcard skip produced %d log entries", len(log.entries))
	}
}

func TestLogHandlerRecordsRoutePattern(t *testing.T) {
	log := &recordingLogger{}
	logFn := LogHandlerWith(log)(func(http.ResponseWriter, *http.Request) {})

	request := httptest.NewRequest(http.MethodGet, "/users/42", nil)
	request = request.WithContext(ContextWithPattern(request.Context(), "GET /users/{id}"))
	logFn.ServeHTTP(httptest.NewRecorder(), request)

	if len(log.entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(log.entries))
	}
	if got := log.entries[0].fields["pattern"]; got != "GET /users/{id}" {
		t.Fatalf("pattern field = %v", got)
	}
}

type countingLogger struct {
	infos, warns, errors int
}

func (c *countingLogger) Debug(string, ...logger.Field) {}
func (c *countingLogger) Info(string, ...logger.Field)  { c.infos++ }
func (c *countingLogger) Warn(string, ...logger.Field)  { c.warns++ }
func (c *countingLogger) Error(string, ...logger.Field) { c.errors++ }
func (c *countingLogger) Panic(string, ...logger.Field) {}
func (c *countingLogger) Fatal(string, ...logger.Field) {}

func (c *countingLogger) Debugf(string, ...any) {}
func (c *countingLogger) Infof(string, ...any)  {}
func (c *countingLogger) Warnf(string, ...any)  {}
func (c *countingLogger) Errorf(string, ...any) {}
func (c *countingLogger) Panicf(string, ...any) {}
func (c *countingLogger) Fatalf(string, ...any) {}

func (c *countingLogger) DebugCtx(context.Context, string, ...logger.Field) {}
func (c *countingLogger) InfoCtx(context.Context, string, ...logger.Field)  { c.infos++ }
func (c *countingLogger) WarnCtx(context.Context, string, ...logger.Field)  { c.warns++ }
func (c *countingLogger) ErrorCtx(context.Context, string, ...logger.Field) { c.errors++ }
func (c *countingLogger) PanicCtx(context.Context, string, ...logger.Field) {}
func (c *countingLogger) FatalCtx(context.Context, string, ...logger.Field) {}

func (c *countingLogger) DebugfCtx(context.Context, string, ...any) {}
func (c *countingLogger) InfofCtx(context.Context, string, ...any)  {}
func (c *countingLogger) WarnfCtx(context.Context, string, ...any)  {}
func (c *countingLogger) ErrorfCtx(context.Context, string, ...any) {}
func (c *countingLogger) PanicfCtx(context.Context, string, ...any) {}
func (c *countingLogger) FatalfCtx(context.Context, string, ...any) {}

func (c *countingLogger) Sync() error { return nil }

func TestLogHandlerUsesGlobalLogger(t *testing.T) {
	counter := &countingLogger{}
	previous := logger.GetGlobal()
	logger.SetGlobal(counter)
	t.Cleanup(func() { logger.SetGlobal(previous) })

	logFn := LogHandler()(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	})
	logFn.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/users", nil))

	if counter.errors != 1 {
		t.Fatalf("global logger errors = %d, want 1", counter.errors)
	}
}

func TestLogHandlerGlobalLoggerLevels(t *testing.T) {
	counter := &countingLogger{}
	previous := logger.GetGlobal()
	logger.SetGlobal(counter)
	t.Cleanup(func() { logger.SetGlobal(previous) })

	logFn := LogHandler()(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	logFn.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/missing", nil))
	if counter.warns != 1 {
		t.Fatalf("warns = %d, want 1", counter.warns)
	}

	okFn := LogHandler()(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	okFn.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/ok", nil))
	if counter.infos != 1 {
		t.Fatalf("infos = %d, want 1", counter.infos)
	}
}

func TestLogRecordsPanickingRequestAndRepanics(t *testing.T) {
	log := &recordingLogger{}
	handlerFn := LogHandlerWith(log)(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	})

	func() {
		defer func() {
			if recovered := recover(); recovered != "boom" {
				t.Fatalf("panic = %v, want boom", recovered)
			}
		}()
		handlerFn.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/users", nil))
	}()

	if len(log.entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(log.entries))
	}
	entry := log.entries[0]
	if entry.level != "error" || entry.fields["status"] != int64(http.StatusInternalServerError) {
		t.Fatalf("entry = %+v, want 500 error record", entry)
	}
}

func TestLogHandlerClientIPOverride(t *testing.T) {
	log := &recordingLogger{}
	handlerFn := LogHandlerWith(log, WithLogClientIP(func(*http.Request) string {
		return "203.0.113.42"
	}))(func(http.ResponseWriter, *http.Request) {})

	handlerFn.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/users", nil))

	if len(log.entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(log.entries))
	}
	if got := log.entries[0].fields["ip"]; got != "203.0.113.42" {
		t.Fatalf("ip = %v, want the configured resolver", got)
	}
}

func TestLogHandlerIgnoresForwardingHeadersByDefault(t *testing.T) {
	log := &recordingLogger{}
	handlerFn := LogHandlerWith(log)(func(http.ResponseWriter, *http.Request) {})

	request := httptest.NewRequest(http.MethodGet, "/users", nil)
	request.RemoteAddr = "10.0.0.1:1234"
	request.Header.Set("X-Forwarded-For", "203.0.113.9")
	handlerFn.ServeHTTP(httptest.NewRecorder(), request)

	if len(log.entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(log.entries))
	}
	if got := log.entries[0].fields["ip"]; got != "10.0.0.1" {
		t.Fatalf("ip = %v, want the direct peer", got)
	}
}
