// Package storage opens and creates the files the commands read and write,
// which may be local paths, "-" for stdin/stdout, or gs://bucket/object.
package storage

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// ErrExist is returned when creating a file that exists without Force.
var ErrExist = errors.New("already exists, use --force to overwrite")

// ErrNotExist is returned when a file does not exist.
var ErrNotExist = os.ErrNotExist

// CreateOptions controls Create.
type CreateOptions struct {
	// Force allows an existing file to be overwritten.
	Force bool
	// LastModified, when set, records the Last-Modified time of the source
	// so a later download can be skipped if the source has not changed.
	LastModified time.Time
}

// Writer is a file being created. Nothing is visible at the target until
// Commit returns without error; Abort discards what has been written.
// Abort after Commit does nothing, so it can be deferred.
type Writer interface {
	io.Writer
	Commit() error
	Abort()
}

// Backend is one kind of storage.
type Backend interface {
	Open(ctx context.Context, path string) (io.ReadCloser, error)
	Create(ctx context.Context, path string, opts CreateOptions) (Writer, error)
	// LastModified returns the recorded Last-Modified time of the source of
	// path, the zero time if none was recorded, or ErrNotExist.
	LastModified(ctx context.Context, path string) (time.Time, error)
}

// Stdio is the path that means stdin or stdout.
const Stdio = "-"

const gcsPrefix = "gs://"

// Router dispatches each path to the backend that handles it.
type Router struct {
	Stdin  io.Reader
	Stdout io.Writer
	Local  Backend
	// NewGCS creates the GCS backend on first use, so commands working on
	// local files need no Google credentials.
	NewGCS func(ctx context.Context) (Backend, io.Closer, error)

	once      sync.Once
	gcs       Backend
	gcsCloser io.Closer
	gcsErr    error
}

// NewRouter returns a Router using stdin and stdout for "-", the local
// filesystem, and Google Cloud Storage with Application Default Credentials.
func NewRouter(stdin io.Reader, stdout io.Writer) *Router {
	return &Router{
		Stdin:  stdin,
		Stdout: stdout,
		Local:  Local{},
		NewGCS: NewGCS,
	}
}

func (r *Router) backend(ctx context.Context, path string) (Backend, error) {
	if !strings.HasPrefix(path, gcsPrefix) {
		return r.Local, nil
	}
	r.once.Do(func() {
		r.gcs, r.gcsCloser, r.gcsErr = r.NewGCS(ctx)
	})
	return r.gcs, r.gcsErr
}

// Open opens path for reading.
func (r *Router) Open(ctx context.Context, path string) (io.ReadCloser, error) {
	if path == Stdio {
		return io.NopCloser(r.Stdin), nil
	}
	b, err := r.backend(ctx, path)
	if err != nil {
		return nil, err
	}
	return b.Open(ctx, path)
}

// Create creates path for writing.
func (r *Router) Create(ctx context.Context, path string, opts CreateOptions) (Writer, error) {
	if path == Stdio {
		return stdoutWriter{r.Stdout}, nil
	}
	b, err := r.backend(ctx, path)
	if err != nil {
		return nil, err
	}
	return b.Create(ctx, path, opts)
}

// LastModified returns the recorded Last-Modified time of the source of
// path, the zero time if none was recorded, or ErrNotExist.
func (r *Router) LastModified(ctx context.Context, path string) (time.Time, error) {
	if path == Stdio {
		return time.Time{}, ErrNotExist
	}
	b, err := r.backend(ctx, path)
	if err != nil {
		return time.Time{}, err
	}
	return b.LastModified(ctx, path)
}

// Close releases the GCS client, if one was created.
func (r *Router) Close() error {
	if r.gcsCloser != nil {
		return r.gcsCloser.Close()
	}
	return nil
}

type stdoutWriter struct{ io.Writer }

func (stdoutWriter) Commit() error { return nil }
func (stdoutWriter) Abort()        {}
