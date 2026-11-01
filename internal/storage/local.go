package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// Local is the local filesystem. A Last-Modified time is recorded as the
// file's modification time.
type Local struct{}

// Open opens path for reading.
func (Local) Open(_ context.Context, path string) (io.ReadCloser, error) {
	return os.Open(path)
}

// Create writes to a temporary file next to path and renames it over path
// on Commit, so a failed run never leaves a partial file behind.
func (Local) Create(_ context.Context, path string, opts CreateOptions) (Writer, error) {
	if !opts.Force {
		if _, err := os.Stat(path); err == nil {
			return nil, fmt.Errorf("%s %w", path, ErrExist)
		}
	}
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return nil, err
	}
	return &localWriter{File: f, path: path, opts: opts}, nil
}

// LastModified returns the modification time of path.
func (Local) LastModified(_ context.Context, path string) (time.Time, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return time.Time{}, ErrNotExist
	}
	if err != nil {
		return time.Time{}, err
	}
	return info.ModTime(), nil
}

type localWriter struct {
	*os.File
	path string
	opts CreateOptions
	done bool
}

func (w *localWriter) Commit() error {
	w.done = true
	if err := w.finish(); err != nil {
		_ = os.Remove(w.Name())
		return err
	}
	return nil
}

func (w *localWriter) finish() error {
	if err := w.Close(); err != nil {
		return err
	}
	if err := os.Chmod(w.Name(), 0o644); err != nil {
		return err
	}
	if !w.opts.LastModified.IsZero() {
		if err := os.Chtimes(w.Name(), w.opts.LastModified, w.opts.LastModified); err != nil {
			return err
		}
	}
	return os.Rename(w.Name(), w.path)
}

func (w *localWriter) Abort() {
	if w.done {
		return
	}
	w.done = true
	_ = w.Close()
	_ = os.Remove(w.Name())
}
