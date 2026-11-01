// Package fetch downloads a file over HTTP, skipping the download when the
// server copy has not changed.
package fetch

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Response is a download in progress.
type Response struct {
	Body io.ReadCloser
	// LastModified is the server's Last-Modified time, or zero if absent.
	LastModified time.Time
	// ContentLength is the size of Body, or -1 if unknown.
	ContentLength int64
}

// Get requests url. When since is not zero the request is conditional, and
// Get returns a nil Response if the server copy is not newer than since.
func Get(ctx context.Context, client *http.Client, url string, since time.Time) (*Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if !since.IsZero() {
		req.Header.Set("If-Modified-Since", since.UTC().Format(http.TimeFormat))
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotModified:
		_ = resp.Body.Close()
		return nil, nil
	default:
		_ = resp.Body.Close()
		return nil, fmt.Errorf("download %s: %s", url, resp.Status)
	}
	var lastModified time.Time
	if v := resp.Header.Get("Last-Modified"); v != "" {
		if t, err := http.ParseTime(v); err == nil {
			lastModified = t
		}
	}
	return &Response{Body: resp.Body, LastModified: lastModified, ContentLength: resp.ContentLength}, nil
}

// Progress is an io.Writer that counts the bytes written through it and
// reports them to Out at most once per Interval.
type Progress struct {
	Out      io.Writer
	Total    int64
	Interval time.Duration

	written int64
	last    time.Time
}

func (p *Progress) Write(b []byte) (int, error) {
	p.written += int64(len(b))
	if now := time.Now(); now.Sub(p.last) >= p.Interval {
		p.last = now
		p.report()
	}
	return len(b), nil
}

// Done writes the final count and ends the progress line.
func (p *Progress) Done() {
	p.report()
	fmt.Fprintln(p.Out)
}

func (p *Progress) report() {
	const mib = 1 << 20
	if p.Total > 0 {
		fmt.Fprintf(p.Out, "\rdownloading %.1f / %.1f MiB (%d%%)",
			float64(p.written)/mib, float64(p.Total)/mib, p.written*100/p.Total)
		return
	}
	fmt.Fprintf(p.Out, "\rdownloading %.1f MiB", float64(p.written)/mib)
}
