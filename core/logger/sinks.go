package logger

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// sinks groups the three outputs a logger writes to: access, error and severe.
// Each output is buffered independently so access logging never blocks error
// logging.
type sinks struct {
	access *bufferedWriter
	errOut *bufferedWriter
	severe *bufferedWriter
	// closers holds the rotating writers, which must be closed (and fsynced)
	// when the logger shuts down. Standard streams are never closed.
	closers []io.Closer
}

// newSinks builds the outputs for Config.Mode.
func newSinks(c Config) sinks {
	switch c.Mode {
	case ModeFile:
		return fileSinks(c, c.Path)
	case ModeVolume:
		if c.ServiceName == "" {
			fmt.Fprintln(os.Stderr, "logger: volume mode requires ServiceName; using file mode")
			return fileSinks(c, c.Path)
		}
		host, _ := os.Hostname()
		return fileSinks(c, filepath.Join(c.Path, c.ServiceName, host))
	default:
		return consoleSinks()
	}
}

// consoleSinks writes to the standard streams.
func consoleSinks() sinks {
	return sinks{
		access: newBufferedWriter(os.Stdout, flushSize),
		errOut: newBufferedWriter(os.Stderr, flushSize),
		severe: newBufferedWriter(os.Stderr, flushSize),
	}
}

// fileSinks writes rotating files under dir.
func fileSinks(c Config, dir string) sinks {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "logger: failed to create log directory %q: %v\n", dir, err)
		return consoleSinks()
	}

	var closer []io.Closer
	open := func(name string) io.Writer {
		writer, err := newRotatingWriter(filepath.Join(dir, name), c)
		if err != nil {
			fmt.Fprintf(os.Stderr, "logger: failed to open %q: %v\n", name, err)
			return os.Stderr
		}
		closer = append(closer, writer)
		return writer
	}
	return sinks{
		access:  newBufferedWriter(open("access.log"), flushSize),
		errOut:  newBufferedWriter(open("error.log"), flushSize),
		severe:  newBufferedWriter(open("severe.log"), flushSize),
		closers: closer,
	}
}

// all returns the buffered outputs.
func (s sinks) all() []*bufferedWriter {
	return []*bufferedWriter{s.access, s.errOut, s.severe}
}

// pick returns the output for level.
func (s sinks) pick(level Level) *bufferedWriter {
	switch {
	case level >= FatalLevel:
		return s.severe
	case level >= ErrorLevel:
		return s.errOut
	default:
		return s.access
	}
}
