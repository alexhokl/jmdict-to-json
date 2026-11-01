package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"cloud.google.com/go/storage"
	"google.golang.org/api/googleapi"
)

// lastModifiedKey is the custom object metadata holding the Last-Modified
// time of the source, in HTTP date format.
const lastModifiedKey = "source-last-modified"

// GCS is Google Cloud Storage, addressed as gs://bucket/object.
type GCS struct {
	Client *storage.Client
}

// NewGCS creates a GCS backend using Application Default Credentials.
func NewGCS(ctx context.Context) (Backend, io.Closer, error) {
	client, err := storage.NewClient(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("create storage client: %w", err)
	}
	return GCS{Client: client}, client, nil
}

// ParseGCSPath splits gs://bucket/object into its bucket and object names.
func ParseGCSPath(path string) (bucket, object string, err error) {
	rest, ok := strings.CutPrefix(path, gcsPrefix)
	bucket, object, _ = strings.Cut(rest, "/")
	if !ok || bucket == "" || object == "" {
		return "", "", fmt.Errorf("invalid GCS path %q, want gs://bucket/object", path)
	}
	return bucket, object, nil
}

func (g GCS) object(path string) (*storage.ObjectHandle, error) {
	bucket, object, err := ParseGCSPath(path)
	if err != nil {
		return nil, err
	}
	return g.Client.Bucket(bucket).Object(object), nil
}

// Open opens path for reading.
func (g GCS) Open(ctx context.Context, path string) (io.ReadCloser, error) {
	obj, err := g.object(path)
	if err != nil {
		return nil, err
	}
	r, err := obj.NewReader(ctx)
	if errors.Is(err, storage.ErrObjectNotExist) {
		return nil, fmt.Errorf("open %s: %w", path, ErrNotExist)
	}
	return r, err
}

// Create uploads to path. GCS only makes an object visible once its upload
// completes, so no temporary object is needed. Without Force the upload is
// made conditional on the object not existing, so the check and the write
// cannot race.
func (g GCS) Create(ctx context.Context, path string, opts CreateOptions) (Writer, error) {
	obj, err := g.object(path)
	if err != nil {
		return nil, err
	}
	if !opts.Force {
		// Checked up front as well so the command fails before doing any work.
		if _, err := obj.Attrs(ctx); err == nil {
			return nil, fmt.Errorf("%s %w", path, ErrExist)
		}
		obj = obj.If(storage.Conditions{DoesNotExist: true})
	}
	ctx, cancel := context.WithCancel(ctx)
	w := obj.NewWriter(ctx)
	if !opts.LastModified.IsZero() {
		w.Metadata = map[string]string{
			lastModifiedKey: opts.LastModified.UTC().Format(http.TimeFormat),
		}
	}
	return &gcsWriter{Writer: w, cancel: cancel, path: path}, nil
}

// LastModified returns the source Last-Modified time recorded in the
// object's metadata.
func (g GCS) LastModified(ctx context.Context, path string) (time.Time, error) {
	obj, err := g.object(path)
	if err != nil {
		return time.Time{}, err
	}
	attrs, err := obj.Attrs(ctx)
	if errors.Is(err, storage.ErrObjectNotExist) {
		return time.Time{}, ErrNotExist
	}
	if err != nil {
		return time.Time{}, err
	}
	value, ok := attrs.Metadata[lastModifiedKey]
	if !ok {
		return time.Time{}, nil
	}
	t, err := http.ParseTime(value)
	if err != nil {
		// An unreadable value only costs a download, so it is not an error.
		return time.Time{}, nil //nolint:nilerr
	}
	return t, nil
}

type gcsWriter struct {
	*storage.Writer
	cancel context.CancelFunc
	path   string
	done   bool
}

func (w *gcsWriter) Commit() error {
	w.done = true
	defer w.cancel()
	err := w.Close()
	var apiErr *googleapi.Error
	if errors.As(err, &apiErr) && apiErr.Code == http.StatusPreconditionFailed {
		return fmt.Errorf("%s %w", w.path, ErrExist)
	}
	return err
}

// Abort cancels the upload, which leaves the existing object untouched.
func (w *gcsWriter) Abort() {
	if w.done {
		return
	}
	w.done = true
	w.cancel()
	_ = w.Close()
}
