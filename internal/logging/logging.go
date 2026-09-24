package logging

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	MaxBytes  int64 = 10 << 20
	KeepFiles       = 5
)

type Sink struct {
	writer *rotatingWriter
	logger *slog.Logger
}

func DefaultDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".oplbm", "logs"), nil
}

func Open(dir string) (*Sink, error) {
	return open(dir, time.Now(), MaxBytes, KeepFiles)
}

func open(dir string, now time.Time, maxBytes int64, keep int) (*Sink, error) {
	if dir == "" {
		return nil, fmt.Errorf("log directory is empty")
	}
	if maxBytes <= 0 {
		return nil, fmt.Errorf("log size limit must be positive")
	}
	if keep <= 0 {
		return nil, fmt.Errorf("log retention must be positive")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	writer := &rotatingWriter{
		dir: dir, date: now.Format("20060102"), maxBytes: maxBytes, keep: keep,
		clock: time.Now,
	}
	if err := writer.openCurrent(); err != nil {
		return nil, err
	}
	return &Sink{
		writer: writer,
		logger: slog.New(slog.NewJSONHandler(writer, &slog.HandlerOptions{Level: slog.LevelInfo})),
	}, nil
}

func (s *Sink) Logger() *slog.Logger { return s.logger }

func (s *Sink) Close() error { return s.writer.close() }

type rotatingWriter struct {
	mu       sync.Mutex
	dir      string
	date     string
	maxBytes int64
	keep     int
	clock    func() time.Time
	file     *os.File
	size     int64
	closed   bool
}

func (w *rotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return 0, os.ErrClosed
	}
	date := w.clock().Format("20060102")
	if date != w.date {
		if err := w.switchDate(date); err != nil {
			return 0, err
		}
	}
	if w.size > 0 && w.size+int64(len(p)) > w.maxBytes {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := w.file.Write(p)
	w.size += int64(n)
	return n, err
}

func (w *rotatingWriter) close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	if w.file == nil {
		return nil
	}
	return w.file.Close()
}

func (w *rotatingWriter) openCurrent() error {
	file, size, err := openFile(w.dir, w.date)
	if err != nil {
		return err
	}
	w.file = file
	w.size = size
	return nil
}

func (w *rotatingWriter) switchDate(date string) error {
	file, size, err := openFile(w.dir, date)
	if err != nil {
		return err
	}
	old := w.file
	w.file = file
	w.size = size
	w.date = date
	if old == nil {
		return nil
	}
	return old.Close()
}

func (w *rotatingWriter) rotate() error {
	if w.file != nil {
		if err := w.file.Close(); err != nil {
			w.file = nil
			return err
		}
		w.file = nil
	}
	current := logPath(w.dir, w.date)
	fail := func(err error) error {
		_ = w.openCurrent()
		return err
	}
	oldest := current + fmt.Sprintf(".%d", w.keep)
	if err := os.Remove(oldest); err != nil && !os.IsNotExist(err) {
		return fail(err)
	}
	for i := w.keep - 1; i >= 1; i-- {
		from := current + fmt.Sprintf(".%d", i)
		to := current + fmt.Sprintf(".%d", i+1)
		if err := renameIfPresent(from, to); err != nil {
			return fail(err)
		}
	}
	if err := renameIfPresent(current, current+".1"); err != nil {
		return fail(err)
	}
	return w.openCurrent()
}

func openFile(dir, date string) (*os.File, int64, error) {
	path := logPath(dir, date)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, 0, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, 0, err
	}
	return file, info.Size(), nil
}

func renameIfPresent(from, to string) error {
	err := os.Rename(from, to)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func logPath(dir, date string) string {
	return filepath.Join(dir, "oplbm-"+date+".log")
}
