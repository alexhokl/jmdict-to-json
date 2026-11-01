package fetch

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

var modified = time.Date(2026, 11, 1, 3, 30, 27, 0, time.UTC)

func newServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "JMdict_e.gz", modified, strings.NewReader("content"))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestGet(t *testing.T) {
	srv := newServer(t)
	resp, err := Get(context.Background(), srv.Client(), srv.URL, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "content" || !resp.LastModified.Equal(modified) || resp.ContentLength != 7 {
		t.Errorf("got %q, %v, %d", body, resp.LastModified, resp.ContentLength)
	}
}

func TestGetNotModified(t *testing.T) {
	srv := newServer(t)
	resp, err := Get(context.Background(), srv.Client(), srv.URL, modified)
	if err != nil || resp != nil {
		t.Errorf("got %v, %v; want nil, nil", resp, err)
	}
}

func TestGetModifiedSince(t *testing.T) {
	srv := newServer(t)
	resp, err := Get(context.Background(), srv.Client(), srv.URL, modified.Add(-24*time.Hour))
	if err != nil || resp == nil {
		t.Fatalf("got %v, %v; want a response", resp, err)
	}
	_ = resp.Body.Close()
}

func TestGetError(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	if _, err := Get(context.Background(), srv.Client(), srv.URL, time.Time{}); err == nil {
		t.Error("expected an error for 404")
	}
}

func TestProgress(t *testing.T) {
	var out bytes.Buffer
	p := &Progress{Out: &out, Total: 2 << 20}
	_, _ = p.Write(make([]byte, 1<<20))
	p.Done()
	if !strings.Contains(out.String(), "1.0 / 2.0 MiB (50%)") || !strings.HasSuffix(out.String(), "\n") {
		t.Errorf("progress = %q", out.String())
	}
}

func TestGetRequestErrors(t *testing.T) {
	ctx := context.Background()
	if _, err := Get(ctx, http.DefaultClient, "://bad url", time.Time{}); err == nil {
		t.Error("expected an error for an invalid URL")
	}
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close() // nothing listens any more
	if _, err := Get(ctx, srv.Client(), srv.URL, time.Time{}); err == nil {
		t.Error("expected an error for a refused connection")
	}
}

func TestGetWithoutLastModified(t *testing.T) {
	for _, header := range []string{"", "not a date"} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if header != "" {
				w.Header().Set("Last-Modified", header)
			}
			_, _ = io.WriteString(w, "content")
		}))
		resp, err := Get(context.Background(), srv.Client(), srv.URL, time.Time{})
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		srv.Close()
		if !resp.LastModified.IsZero() {
			t.Errorf("Last-Modified %q: got %v, want zero", header, resp.LastModified)
		}
	}
}

func TestGetSendsIfModifiedSince(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("If-Modified-Since")
		w.WriteHeader(http.StatusNotModified)
	}))
	defer srv.Close()
	since := time.Date(2026, 11, 1, 12, 30, 27, 0, time.FixedZone("JST", 9*3600))
	if _, err := Get(context.Background(), srv.Client(), srv.URL, since); err != nil {
		t.Fatal(err)
	}
	if got != "Sun, 01 Nov 2026 03:30:27 GMT" {
		t.Errorf("If-Modified-Since = %q", got)
	}
}

func TestProgressUnknownTotal(t *testing.T) {
	var out bytes.Buffer
	p := &Progress{Out: &out, Total: -1, Interval: time.Hour}
	_, _ = p.Write(make([]byte, 3<<19))
	_, _ = p.Write(make([]byte, 1<<19)) // within Interval: not reported
	p.Done()
	if got := out.String(); got != "\rdownloading 1.5 MiB\rdownloading 2.0 MiB\n" {
		t.Errorf("progress = %q", got)
	}
}
