package logger

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
	"time"
)

// benchLogger returns a file-mode logger writing into a temporary directory.
// Close runs in cleanup so buffered entries and the flusher goroutine are
// released after each benchmark.
func benchLogger(b *testing.B, cfg Config) *Logger {
	b.Helper()
	cfg.Mode = ModeFile
	cfg.Path = b.TempDir()
	l := New(cfg).(*Logger)
	b.Cleanup(func() { _ = l.Close() })
	return l
}

func BenchmarkLoggerInfoSequential(b *testing.B) {
	l := benchLogger(b, Config{Level: "info"})
	field := String("key", "value")

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.Info("benchmark entry", field)
	}
}

func BenchmarkLoggerInfoParallel(b *testing.B) {
	l := benchLogger(b, Config{Level: "info"})
	field := String("key", "value")

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			l.Info("benchmark entry", field)
		}
	})
}

func BenchmarkLoggerInfoCtxParallel(b *testing.B) {
	l := benchLogger(b, Config{Level: "info"})
	unregister := RegisterContextExtractor(func(context.Context) []Field {
		return []Field{String("request_id", "0f3c9a7e"), String("trace_id", "b1c2d3")}
	})
	defer unregister()

	ctx := context.Background()
	field := String("key", "value")

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			l.InfoCtx(ctx, "benchmark entry", field)
		}
	})
}

// BenchmarkLoggerFiltered measures the cost of a disabled level: the call must
// return before encoding or touching a sink.
func BenchmarkLoggerFiltered(b *testing.B) {
	l := benchLogger(b, Config{Level: "info"})

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.Debug("dropped entry")
	}
}

// benchEncode measures encoding alone with a reused buffer, which is what the
// log path does (the buffer comes from encodeBufPool there).
func benchEncode(b *testing.B, encoding string) {
	l := benchLogger(b, Config{Level: "info", Encoding: encoding})
	now := time.Now()
	fields := []Field{String("method", "GET"), String("path", "/users/42"), Int("status", 200)}
	buf := new(bytes.Buffer)
	e := entry{level: InfoLevel, now: now, app: l.config.ServiceName, msg: "http request", fields: fields}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		l.enc.encodeInto(buf, e)
	}
}

func BenchmarkLoggerEncodeJSON(b *testing.B)  { benchEncode(b, JSONEncoding) }
func BenchmarkLoggerEncodePlain(b *testing.B) { benchEncode(b, ConsoleEncoding) }

func BenchmarkRotatingWriterWrite(b *testing.B) {
	path := filepath.Join(b.TempDir(), "access.log")
	w, err := newRotatingWriter(path, Config{Rotation: RotationSize, MaxSize: 1 << 20})
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = w.Close() }()

	line := []byte(`{"level":"INFO","msg":"benchmark entry","key":"value"}` + "\n")

	b.SetBytes(int64(len(line)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := w.Write(line); err != nil {
			b.Fatal(err)
		}
	}
}
