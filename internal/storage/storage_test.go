package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLocalCreateCommit(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "out.txt")
	modified := time.Date(2026, 11, 1, 3, 30, 27, 0, time.UTC)

	w, err := Local{}.Create(ctx, path, CreateOptions{LastModified: modified})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(w, "hello"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("target visible before Commit: %v", err)
	}
	if err := w.Commit(); err != nil {
		t.Fatal(err)
	}
	w.Abort() // no-op after Commit

	got, err := os.ReadFile(path)
	if err != nil || string(got) != "hello" {
		t.Errorf("content = %q, %v", got, err)
	}
	lm, err := Local{}.LastModified(ctx, path)
	if err != nil || !lm.Equal(modified) {
		t.Errorf("LastModified = %v, %v; want %v", lm, err, modified)
	}
	assertNoTempFiles(t, filepath.Dir(path))
}

func TestLocalCreateRefusesExisting(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "out.txt")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := (Local{}).Create(ctx, path, CreateOptions{}); !errors.Is(err, ErrExist) {
		t.Fatalf("Create without Force = %v, want ErrExist", err)
	}

	w, err := Local{}.Create(ctx, path, CreateOptions{Force: true})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(w, "new")
	if err := w.Commit(); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != "new" {
		t.Errorf("content = %q, want new", got)
	}
}

func TestLocalAbort(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")
	w, err := Local{}.Create(context.Background(), path, CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(w, "partial")
	w.Abort()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("target exists after Abort: %v", err)
	}
	assertNoTempFiles(t, dir)
}

func TestLocalLastModifiedMissing(t *testing.T) {
	_, err := Local{}.LastModified(context.Background(), filepath.Join(t.TempDir(), "none"))
	if !errors.Is(err, ErrNotExist) {
		t.Errorf("err = %v, want ErrNotExist", err)
	}
}

func assertNoTempFiles(t *testing.T, dir string) {
	t.Helper()
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("temporary file left behind: %s", e.Name())
		}
	}
}

func TestParseGCSPath(t *testing.T) {
	bucket, object, err := ParseGCSPath("gs://my-bucket/dir/jmdict.gz")
	if err != nil || bucket != "my-bucket" || object != "dir/jmdict.gz" {
		t.Errorf("got %q, %q, %v", bucket, object, err)
	}
	for _, bad := range []string{"gs://", "gs://bucket", "gs://bucket/", "gs:///object", "/local/path"} {
		if _, _, err := ParseGCSPath(bad); err == nil {
			t.Errorf("ParseGCSPath(%q) succeeded", bad)
		}
	}
}

type fakeBackend struct {
	name  string
	calls []string
}

func (f *fakeBackend) Open(_ context.Context, path string) (io.ReadCloser, error) {
	f.calls = append(f.calls, "open "+path)
	return io.NopCloser(strings.NewReader(f.name)), nil
}

func (f *fakeBackend) Create(_ context.Context, path string, _ CreateOptions) (Writer, error) {
	f.calls = append(f.calls, "create "+path)
	return stdoutWriter{io.Discard}, nil
}

func (f *fakeBackend) LastModified(_ context.Context, path string) (time.Time, error) {
	f.calls = append(f.calls, "lastModified "+path)
	return time.Time{}, nil
}

func TestRouter(t *testing.T) {
	ctx := context.Background()
	local := &fakeBackend{name: "local"}
	gcs := &fakeBackend{name: "gcs"}
	var stdout bytes.Buffer
	gcsCreated := 0
	r := &Router{
		Stdin:  strings.NewReader("stdin"),
		Stdout: &stdout,
		Local:  local,
		NewGCS: func(context.Context) (Backend, io.Closer, error) {
			gcsCreated++
			return gcs, nil, nil
		},
	}

	read := func(path string) string {
		rc, err := r.Open(ctx, path)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = rc.Close() }()
		b, _ := io.ReadAll(rc)
		return string(b)
	}
	if got := read("jmdict.xml"); got != "local" {
		t.Errorf("local path read from %q", got)
	}
	if gcsCreated != 0 {
		t.Error("GCS backend created for a local path")
	}
	if got := read("gs://b/o"); got != "gcs" {
		t.Errorf("gs:// path read from %q", got)
	}
	if got := read("-"); got != "stdin" {
		t.Errorf("- read from %q", got)
	}
	if _, err := r.Create(ctx, "gs://b/o2", CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if gcsCreated != 1 {
		t.Errorf("GCS backend created %d times, want 1", gcsCreated)
	}

	w, err := r.Create(ctx, "-", CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(w, "out")
	if err := w.Commit(); err != nil || stdout.String() != "out" {
		t.Errorf("stdout = %q, %v", stdout.String(), err)
	}
	if _, err := r.LastModified(ctx, "-"); !errors.Is(err, ErrNotExist) {
		t.Errorf("LastModified(-) = %v, want ErrNotExist", err)
	}
	if len(gcs.calls) != 2 || gcs.calls[1] != "create gs://b/o2" {
		t.Errorf("gcs calls = %v", gcs.calls)
	}
}

func TestLocalOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "in.txt")
	_ = os.WriteFile(path, []byte("data"), 0o644)
	rc, err := Local{}.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rc.Close() }()
	if b, _ := io.ReadAll(rc); string(b) != "data" {
		t.Errorf("read %q", b)
	}
	if _, err := (Local{}).Open(context.Background(), path+".missing"); !errors.Is(err, ErrNotExist) {
		t.Errorf("Open missing = %v, want ErrNotExist", err)
	}
}

func TestLocalCreateInMissingDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "out.txt")
	if _, err := (Local{}).Create(context.Background(), path, CreateOptions{}); err == nil {
		t.Error("expected an error for a missing directory")
	}
}

// A failed Commit must not leave the temporary file behind.
func TestLocalCommitFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out")
	w, err := Local{}.Create(context.Background(), path, CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// A non-empty directory at the target makes the rename fail.
	if err := os.MkdirAll(filepath.Join(path, "child"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := w.Commit(); err == nil {
		t.Fatal("expected Commit to fail")
	}
	assertNoTempFiles(t, dir)
}

func TestLocalLastModifiedError(t *testing.T) {
	// Stat through a regular file fails with ENOTDIR, not ENOENT.
	file := filepath.Join(t.TempDir(), "file")
	_ = os.WriteFile(file, nil, 0o644)
	_, err := Local{}.LastModified(context.Background(), filepath.Join(file, "child"))
	if err == nil || errors.Is(err, ErrNotExist) {
		t.Errorf("err = %v, want a non-NotExist error", err)
	}
}

type closerFunc func() error

func (f closerFunc) Close() error { return f() }

func TestRouterGCSClientLifecycle(t *testing.T) {
	ctx := context.Background()
	closed := false
	r := &Router{
		Local: &fakeBackend{name: "local"},
		NewGCS: func(context.Context) (Backend, io.Closer, error) {
			return &fakeBackend{name: "gcs"}, closerFunc(func() error { closed = true; return nil }), nil
		},
	}
	if err := r.Close(); err != nil || closed {
		t.Errorf("Close before GCS use = %v, closed = %v", err, closed)
	}
	if _, err := r.LastModified(ctx, "gs://b/o"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.LastModified(ctx, "local"); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil || !closed {
		t.Errorf("Close = %v, closed = %v", err, closed)
	}
}

func TestRouterGCSClientError(t *testing.T) {
	ctx := context.Background()
	want := errors.New("no credentials")
	r := &Router{NewGCS: func(context.Context) (Backend, io.Closer, error) { return nil, nil, want }}
	if _, err := r.Open(ctx, "gs://b/o"); !errors.Is(err, want) {
		t.Errorf("Open = %v", err)
	}
	if _, err := r.Create(ctx, "gs://b/o", CreateOptions{}); !errors.Is(err, want) {
		t.Errorf("Create = %v", err)
	}
	if _, err := r.LastModified(ctx, "gs://b/o"); !errors.Is(err, want) {
		t.Errorf("LastModified = %v", err)
	}
}

func TestNewRouterStdio(t *testing.T) {
	var out bytes.Buffer
	r := NewRouter(strings.NewReader("in"), &out)
	w, err := r.Create(context.Background(), Stdio, CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(w, "out")
	w.Abort() // stdout cannot be rolled back; Abort is a no-op
	if out.String() != "out" {
		t.Errorf("stdout = %q", out.String())
	}
	if _, ok := r.Local.(Local); !ok {
		t.Errorf("Local = %T, want Local", r.Local)
	}
}
