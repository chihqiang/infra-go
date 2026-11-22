package logger

import (
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	megaBytes       = int64(1 << 20)
	defaultTimeFile = "2006-01-02T15-04-05.000000000"
	dateOnly        = "2006-01-02"
)

// rotatingWriter is a size/daily rotating file writer. It deliberately uses only the
// standard library and keeps the logger package free of third-party dependencies.
type rotatingWriter struct {
	mu         sync.Mutex
	filename   string
	file       *os.File
	size       int64
	rotation   string
	maxSize    int64
	maxBackups int
	maxAge     time.Duration
	compress   bool
	currentDay string
	closed     bool
}

func newRotatingWriter(filename string, cfg Config) (*rotatingWriter, error) {
	rotation := cfg.Rotation
	if rotation != RotationSize {
		rotation = RotationDaily
	}

	w := &rotatingWriter{
		filename:   filename,
		rotation:   rotation,
		maxSize:    int64(cfg.MaxSize) * megaBytes,
		maxBackups: cfg.MaxBackups,
		maxAge:     time.Duration(cfg.KeepDays) * 24 * time.Hour,
		compress:   cfg.Compress,
	}

	file, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	w.file = file
	if info, statErr := file.Stat(); statErr == nil {
		w.size = info.Size()
	}
	w.currentDay = w.now().Format(dateOnly)
	return w, nil
}

func (w *rotatingWriter) now() time.Time {
	return time.Now()
}

// Write appends p, rotating the current file first when needed.
func (w *rotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return 0, errors.New("logger: log file is closed")
	}

	now := w.now()
	rotate := false
	switch w.rotation {
	case RotationDaily:
		rotate = now.Format(dateOnly) != w.currentDay
	default:
		rotate = w.maxSize > 0 && w.size > 0 && w.size+int64(len(p)) > w.maxSize
	}
	if rotate {
		if err := w.rotateLocked(now); err != nil {
			return 0, err
		}
	}

	n, err := w.file.Write(p)
	w.size += int64(n)
	return n, err
}

// rotateLocked closes the active file, renames it to a timestamped backup, optionally
// compresses it, opens a new active file and applies the retention policy.
func (w *rotatingWriter) rotateLocked(now time.Time) error {
	if err := w.file.Sync(); err != nil {
		return err
	}
	if err := w.file.Close(); err != nil {
		return err
	}

	backup := w.backupName(now)
	if err := os.Rename(w.filename, backup); err != nil {
		// Reopen the active file so the logger keeps working after a failed rename.
		file, openErr := os.OpenFile(w.filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if openErr == nil {
			w.file = file
		}
		return err
	}

	if w.compress {
		compressed := backup + ".gz"
		if err := gzipFile(backup, compressed); err != nil {
			// Keep the uncompressed backup if compression fails.
			_ = os.Remove(compressed)
		} else {
			_ = os.Remove(backup)
		}
	}

	file, err := os.OpenFile(w.filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	w.file = file
	w.size = 0
	w.currentDay = now.Format(dateOnly)
	return w.cleanupLocked(now)
}

func (w *rotatingWriter) backupName(now time.Time) string {
	dir := filepath.Dir(w.filename)
	base := filepath.Base(w.filename)
	ext := filepath.Ext(base)
	prefix := strings.TrimSuffix(base, ext)
	stamp := now.Format(defaultTimeFile)

	name := filepath.Join(dir, prefix+"-"+stamp+ext)
	for i := 1; ; i++ {
		if _, err := os.Stat(name); errors.Is(err, os.ErrNotExist) {
			return name
		}
		name = filepath.Join(dir, prefix+"-"+stamp+"-"+strconv.Itoa(i)+ext)
	}
}

// cleanupLocked removes backups beyond maxBackups and older than maxAge.
func (w *rotatingWriter) cleanupLocked(now time.Time) error {
	if w.maxBackups <= 0 && w.maxAge <= 0 {
		return nil
	}

	dir := filepath.Dir(w.filename)
	base := filepath.Base(w.filename)
	ext := filepath.Ext(base)
	prefix := strings.TrimSuffix(base, ext)
	pattern := filepath.Join(dir, prefix+"-*"+ext+"*")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return err
	}
	sort.Strings(files)

	var errs []error
	if w.maxBackups > 0 && len(files) > w.maxBackups {
		for _, file := range files[:len(files)-w.maxBackups] {
			if removeErr := os.Remove(file); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				errs = append(errs, removeErr)
			}
		}
		files = files[len(files)-w.maxBackups:]
	}

	if w.maxAge > 0 {
		boundary := now.Add(-w.maxAge)
		for _, file := range files {
			info, statErr := os.Stat(file)
			if statErr != nil {
				if errors.Is(statErr, os.ErrNotExist) {
					continue
				}
				errs = append(errs, statErr)
				continue
			}
			if info.ModTime().Before(boundary) {
				if removeErr := os.Remove(file); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
					errs = append(errs, removeErr)
				}
			}
		}
	}
	return errors.Join(errs...)
}

func gzipFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	writer := gzip.NewWriter(out)
	if _, err := io.Copy(writer, in); err != nil {
		_ = writer.Close()
		_ = out.Close()
		return err
	}
	if err := writer.Close(); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// Sync flushes the active file.
func (w *rotatingWriter) Sync() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed || w.file == nil {
		return nil
	}
	return w.file.Sync()
}

// Close flushes and closes the active file. It is idempotent.
func (w *rotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	if w.file == nil {
		return nil
	}
	err := w.file.Sync()
	if closeErr := w.file.Close(); err == nil {
		err = closeErr
	}
	return err
}

func (w *rotatingWriter) String() string {
	return fmt.Sprintf("rotatingWriter(%s)", w.filename)
}
