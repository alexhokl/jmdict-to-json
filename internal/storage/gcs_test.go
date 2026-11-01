package storage

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeGCS implements the few Cloud Storage API calls the GCS backend makes:
// object metadata, XML API download and multipart upload. Objects whose
// name starts with "forbidden" answer 403, which the client does not retry.
type fakeGCS struct {
	mu      sync.Mutex
	objects map[string]fakeObject
}

type fakeObject struct {
	data     []byte
	metadata map[string]string
}

func newFakeGCS(t *testing.T) *fakeGCS {
	t.Helper()
	f := &fakeGCS{objects: make(map[string]fakeObject)}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /storage/v1/b/{bucket}/o/{object...}", f.attrs)
	mux.HandleFunc("GET /{bucket}/{object...}", f.download)
	mux.HandleFunc("POST /upload/storage/v1/b/{bucket}/o", f.upload)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	t.Setenv("STORAGE_EMULATOR_HOST", strings.TrimPrefix(srv.URL, "http://"))
	return f
}

func (f *fakeGCS) put(key string, data string, metadata map[string]string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.objects[key] = fakeObject{data: []byte(data), metadata: metadata}
}

func (f *fakeGCS) get(key string) (fakeObject, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	o, ok := f.objects[key]
	return o, ok
}

func (f *fakeGCS) lookup(w http.ResponseWriter, r *http.Request) (string, fakeObject, bool) {
	bucket, object := r.PathValue("bucket"), r.PathValue("object")
	if strings.HasPrefix(object, "forbidden") {
		writeError(w, http.StatusForbidden)
		return "", fakeObject{}, false
	}
	o, ok := f.get(bucket + "/" + object)
	if !ok {
		writeError(w, http.StatusNotFound)
	}
	return bucket + "/" + object, o, ok
}

func (f *fakeGCS) attrs(w http.ResponseWriter, r *http.Request) {
	key, o, ok := f.lookup(w, r)
	if !ok {
		return
	}
	bucket, name, _ := strings.Cut(key, "/")
	writeObject(w, bucket, name, o)
}

func (f *fakeGCS) download(w http.ResponseWriter, r *http.Request) {
	if _, o, ok := f.lookup(w, r); ok {
		_, _ = w.Write(o.data)
	}
}

func (f *fakeGCS) upload(w http.ResponseWriter, r *http.Request) {
	bucket, name := r.PathValue("bucket"), r.URL.Query().Get("name")
	_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	mr := multipart.NewReader(r.Body, params["boundary"])
	var meta struct {
		Metadata map[string]string `json:"metadata"`
	}
	part, err := mr.NextPart()
	if err == nil {
		err = json.NewDecoder(part).Decode(&meta)
	}
	if err == nil {
		part, err = mr.NextPart()
	}
	var data []byte
	if err == nil {
		data, err = io.ReadAll(part)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	key := bucket + "/" + name
	if _, exists := f.objects[key]; exists && r.URL.Query().Get("ifGenerationMatch") == "0" {
		writeError(w, http.StatusPreconditionFailed)
		return
	}
	o := fakeObject{data: data, metadata: meta.Metadata}
	f.objects[key] = o
	writeObject(w, bucket, name, o)
}

func writeObject(w http.ResponseWriter, bucket, name string, o fakeObject) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"bucket":     bucket,
		"name":       name,
		"size":       strconv.Itoa(len(o.data)),
		"generation": "1",
		"metadata":   o.metadata,
	})
}

func writeError(w http.ResponseWriter, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{"code": code, "message": http.StatusText(code)},
	})
}

func newTestGCS(t *testing.T) (*fakeGCS, Backend) {
	t.Helper()
	fake := newFakeGCS(t)
	b, closer, err := NewGCS(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = closer.Close() })
	return fake, b
}

func TestGCSCreateAndOpen(t *testing.T) {
	ctx := context.Background()
	fake, b := newTestGCS(t)
	modified := time.Date(2026, 11, 1, 3, 30, 27, 0, time.UTC)

	w, err := b.Create(ctx, "gs://bk/dir/jmdict.gz", CreateOptions{LastModified: modified})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(w, "content")
	if err := w.Commit(); err != nil {
		t.Fatal(err)
	}
	w.Abort() // no-op after Commit

	o, ok := fake.get("bk/dir/jmdict.gz")
	if !ok || string(o.data) != "content" {
		t.Fatalf("object = %+v, %v", o, ok)
	}
	if got := o.metadata[lastModifiedKey]; got != "Sun, 01 Nov 2026 03:30:27 GMT" {
		t.Errorf("metadata = %q", got)
	}

	r, err := b.Open(ctx, "gs://bk/dir/jmdict.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	if data, _ := io.ReadAll(r); string(data) != "content" {
		t.Errorf("read %q", data)
	}

	lm, err := b.LastModified(ctx, "gs://bk/dir/jmdict.gz")
	if err != nil || !lm.Equal(modified) {
		t.Errorf("LastModified = %v, %v; want %v", lm, err, modified)
	}
}

func TestGCSCreateWithoutLastModified(t *testing.T) {
	ctx := context.Background()
	fake, b := newTestGCS(t)
	w, err := b.Create(ctx, "gs://bk/out.json", CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Commit(); err != nil {
		t.Fatal(err)
	}
	if o, _ := fake.get("bk/out.json"); len(o.metadata) != 0 {
		t.Errorf("metadata = %v, want none", o.metadata)
	}
}

func TestGCSCreateRefusesExisting(t *testing.T) {
	ctx := context.Background()
	fake, b := newTestGCS(t)
	fake.put("bk/out.json", "old", nil)

	if _, err := b.Create(ctx, "gs://bk/out.json", CreateOptions{}); !errors.Is(err, ErrExist) {
		t.Fatalf("Create without Force = %v, want ErrExist", err)
	}

	w, err := b.Create(ctx, "gs://bk/out.json", CreateOptions{Force: true})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(w, "new")
	if err := w.Commit(); err != nil {
		t.Fatal(err)
	}
	if o, _ := fake.get("bk/out.json"); string(o.data) != "new" {
		t.Errorf("content = %q, want new", o.data)
	}
}

// The object appears after the up-front check but before the upload
// finishes; the DoesNotExist precondition must still refuse to overwrite it.
func TestGCSCommitPreconditionFailure(t *testing.T) {
	ctx := context.Background()
	fake, b := newTestGCS(t)
	w, err := b.Create(ctx, "gs://bk/out.json", CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	fake.put("bk/out.json", "theirs", nil)
	_, _ = io.WriteString(w, "mine")
	if err := w.Commit(); !errors.Is(err, ErrExist) {
		t.Fatalf("Commit = %v, want ErrExist", err)
	}
	if o, _ := fake.get("bk/out.json"); string(o.data) != "theirs" {
		t.Errorf("content = %q, want theirs", o.data)
	}
}

func TestGCSAbort(t *testing.T) {
	ctx := context.Background()
	fake, b := newTestGCS(t)
	w, err := b.Create(ctx, "gs://bk/out.json", CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(w, "partial")
	w.Abort()
	w.Abort() // no-op the second time
	if _, ok := fake.get("bk/out.json"); ok {
		t.Error("object created after Abort")
	}
}

func TestGCSLastModified(t *testing.T) {
	ctx := context.Background()
	fake, b := newTestGCS(t)
	fake.put("bk/none", "x", nil)
	fake.put("bk/bad", "x", map[string]string{lastModifiedKey: "not a date"})

	tests := []struct {
		path    string
		wantErr error
	}{
		{"gs://bk/none", nil}, // no recorded time: zero, so download again
		{"gs://bk/bad", nil},  // unreadable time: same
		{"gs://bk/missing", ErrNotExist},
	}
	for _, tt := range tests {
		lm, err := b.LastModified(ctx, tt.path)
		if !errors.Is(err, tt.wantErr) || (err == nil && !lm.IsZero()) {
			t.Errorf("LastModified(%s) = %v, %v; want zero, %v", tt.path, lm, err, tt.wantErr)
		}
	}
}

func TestGCSErrors(t *testing.T) {
	ctx := context.Background()
	_, b := newTestGCS(t)

	if _, err := b.Open(ctx, "gs://bk/missing"); !errors.Is(err, ErrNotExist) {
		t.Errorf("Open missing = %v, want ErrNotExist", err)
	}
	if _, err := b.Open(ctx, "gs://bk/forbidden"); err == nil || errors.Is(err, ErrNotExist) {
		t.Errorf("Open forbidden = %v, want a non-NotExist error", err)
	}
	if _, err := b.LastModified(ctx, "gs://bk/forbidden"); err == nil || errors.Is(err, ErrNotExist) {
		t.Errorf("LastModified forbidden = %v, want a non-NotExist error", err)
	}
	for _, call := range []func() error{
		func() error { _, err := b.Open(ctx, "gs://bk"); return err },
		func() error { _, err := b.Create(ctx, "gs://bk", CreateOptions{}); return err },
		func() error { _, err := b.LastModified(ctx, "gs://bk"); return err },
	} {
		if err := call(); err == nil || !strings.Contains(err.Error(), "invalid GCS path") {
			t.Errorf("err = %v, want invalid GCS path", err)
		}
	}
}

func TestRouterWithGCS(t *testing.T) {
	ctx := context.Background()
	fake := newFakeGCS(t)
	fake.put("bk/in.txt", "from gcs", map[string]string{lastModifiedKey: "Sun, 01 Nov 2026 03:30:27 GMT"})

	r := NewRouter(strings.NewReader(""), io.Discard)
	rc, err := r.Open(ctx, "gs://bk/in.txt")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(rc)
	_ = rc.Close()
	if string(data) != "from gcs" {
		t.Errorf("read %q", data)
	}
	if lm, err := r.LastModified(ctx, "gs://bk/in.txt"); err != nil || lm.IsZero() {
		t.Errorf("LastModified = %v, %v", lm, err)
	}
	if err := r.Close(); err != nil {
		t.Errorf("Close = %v", err)
	}
}
