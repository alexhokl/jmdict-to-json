package cmd

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var modified = time.Date(2026, 11, 1, 3, 30, 27, 0, time.UTC)

func sampleGzip(t *testing.T) []byte {
	t.Helper()
	xml, err := os.ReadFile("../internal/jmdict/testdata/sample.xml")
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, _ = zw.Write(xml)
	_ = zw.Close()
	return buf.Bytes()
}

// execute runs the command line and returns stdout and stderr.
func execute(t *testing.T, stdin string, args ...string) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	err := run(context.Background(), args, strings.NewReader(stdin), &stdout, &stderr)
	return stdout.String(), stderr.String(), err
}

func newServer(t *testing.T, body []byte) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var full atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := httptest.NewRecorder()
		http.ServeContent(rec, r, "JMdict_e.gz", modified, bytes.NewReader(body))
		if rec.Code == http.StatusOK {
			full.Add(1)
		}
		for k, v := range rec.Header() {
			w.Header()[k] = v
		}
		w.WriteHeader(rec.Code)
		_, _ = w.Write(rec.Body.Bytes())
	}))
	t.Cleanup(srv.Close)
	return srv, &full
}

func TestDownload(t *testing.T) {
	body := sampleGzip(t)
	srv, full := newServer(t, body)
	out := filepath.Join(t.TempDir(), "jmdict.gz")

	if _, stderr, err := execute(t, "", "download", "--url", srv.URL, "-o", out); err != nil {
		t.Fatalf("download: %v\n%s", err, stderr)
	}
	got, _ := os.ReadFile(out)
	if !bytes.Equal(got, body) {
		t.Error("downloaded content differs")
	}
	info, _ := os.Stat(out)
	if !info.ModTime().Equal(modified) {
		t.Errorf("mtime = %v, want %v", info.ModTime(), modified)
	}

	_, stderr, err := execute(t, "", "download", "--url", srv.URL, "-o", out)
	if err != nil || !strings.Contains(stderr, "already up to date") {
		t.Errorf("second download: %v\n%s", err, stderr)
	}
	if full.Load() != 1 {
		t.Errorf("full downloads = %d, want 1", full.Load())
	}

	if _, stderr, err := execute(t, "", "download", "--url", srv.URL, "-o", out, "--force"); err != nil {
		t.Fatalf("forced download: %v\n%s", err, stderr)
	}
	if full.Load() != 2 {
		t.Errorf("full downloads = %d, want 2", full.Load())
	}
}

func TestDownloadJSONLog(t *testing.T) {
	srv, _ := newServer(t, sampleGzip(t))
	out := filepath.Join(t.TempDir(), "jmdict.gz")
	_, stderr, err := execute(t, "", "download", "--url", srv.URL, "-o", out, "--log-format", "json")
	if err != nil {
		t.Fatal(err)
	}
	var entry map[string]any
	if err := json.Unmarshal([]byte(stderr), &entry); err != nil {
		t.Fatalf("stderr is not one JSON entry (progress must be off): %q", stderr)
	}
	if entry["message"] != "downloaded" || entry["severity"] != "INFO" {
		t.Errorf("entry = %v", entry)
	}
}

func TestDownloadErrorIsLogged(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	out := filepath.Join(t.TempDir(), "jmdict.gz")
	_, stderr, err := execute(t, "", "download", "--url", srv.URL, "-o", out, "--log-format", "json")
	if err == nil || !strings.Contains(stderr, `"severity":"ERROR"`) {
		t.Errorf("err = %v, stderr = %s", err, stderr)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Error("output created on a failed download")
	}
}

func TestUnzipAndConvert(t *testing.T) {
	dir := t.TempDir()
	gz := filepath.Join(dir, "jmdict.gz")
	xml := filepath.Join(dir, "jmdict.xml")
	jsonPath := filepath.Join(dir, "jmdict.json")
	if err := os.WriteFile(gz, sampleGzip(t), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, stderr, err := execute(t, "", "unzip", "-i", gz, "-o", xml); err != nil {
		t.Fatalf("unzip: %v\n%s", err, stderr)
	}
	want, _ := os.ReadFile("../internal/jmdict/testdata/sample.xml")
	if got, _ := os.ReadFile(xml); !bytes.Equal(got, want) {
		t.Error("unzipped content differs")
	}

	if _, stderr, err := execute(t, "", "convert", "-i", xml, "-o", jsonPath); err != nil {
		t.Fatalf("convert: %v\n%s", err, stderr)
	}
	fromXML, _ := os.ReadFile(jsonPath)

	// The .gz is detected and gives the same JSON.
	stdout, stderr, err := execute(t, "", "convert", "-i", gz, "-o", "-")
	if err != nil {
		t.Fatalf("convert gz: %v\n%s", err, stderr)
	}
	if stdout != string(fromXML) {
		t.Errorf("JSON from .gz differs from JSON from XML")
	}
	var entries []map[string]any
	if err := json.Unmarshal(fromXML, &entries); err != nil || len(entries) != 2 {
		t.Errorf("entries = %d, %v", len(entries), err)
	}
}

func TestConvertStdin(t *testing.T) {
	xml, _ := os.ReadFile("../internal/jmdict/testdata/sample.xml")
	stdout, stderr, err := execute(t, string(xml), "convert", "-i", "-", "-o", "-", "--jsonl")
	if err != nil {
		t.Fatalf("%v\n%s", err, stderr)
	}
	if lines := strings.Count(stdout, "\n"); lines != 2 {
		t.Errorf("lines = %d, want 2:\n%s", lines, stdout)
	}
}

func TestRefusesExistingOutputWithoutForce(t *testing.T) {
	dir := t.TempDir()
	gz := filepath.Join(dir, "jmdict.gz")
	existing := filepath.Join(dir, "existing")
	_ = os.WriteFile(gz, sampleGzip(t), 0o644)

	for _, command := range []string{"unzip", "convert"} {
		_ = os.WriteFile(existing, []byte("keep"), 0o644)
		_, stderr, err := execute(t, "", command, "-i", gz, "-o", existing)
		if err == nil || !strings.Contains(stderr, "--force") {
			t.Errorf("%s: err = %v, stderr = %s", command, err, stderr)
		}
		if got, _ := os.ReadFile(existing); string(got) != "keep" {
			t.Errorf("%s overwrote the output", command)
		}
		if _, stderr, err := execute(t, "", command, "-i", gz, "-o", existing, "--force"); err != nil {
			t.Errorf("%s --force: %v\n%s", command, err, stderr)
		}
	}
}

func TestPrettyAndJSONLAreExclusive(t *testing.T) {
	_, stderr, err := execute(t, "", "convert", "--pretty", "--jsonl", "-o", filepath.Join(t.TempDir(), "x"))
	if err == nil || !strings.Contains(stderr, "pretty") {
		t.Errorf("err = %v, stderr = %s", err, stderr)
	}
}

func TestInvalidLogFormat(t *testing.T) {
	_, stderr, err := execute(t, "", "convert", "--log-format", "xml")
	if err == nil || !strings.Contains(stderr, "invalid log format") {
		t.Errorf("err = %v, stderr = %s", err, stderr)
	}
}

func TestDownloadToStdout(t *testing.T) {
	body := sampleGzip(t)
	srv, _ := newServer(t, body)
	stdout, stderr, err := execute(t, "", "download", "--url", srv.URL, "-o", "-")
	if err != nil {
		t.Fatalf("%v\n%s", err, stderr)
	}
	if stdout != string(body) {
		t.Error("stdout differs from the served file")
	}
}

func TestDownloadErrors(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	_ = os.WriteFile(file, nil, 0o644)

	truncated := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "100")
		_, _ = io.WriteString(w, "short")
	}))
	defer truncated.Close()
	srv, _ := newServer(t, sampleGzip(t))

	tests := map[string][]string{
		// Stat through a regular file fails with ENOTDIR, not "not exist".
		"unreadable output": {"-o", filepath.Join(file, "child"), "--url", srv.URL},
		"missing directory": {"-o", filepath.Join(dir, "missing", "jmdict.gz"), "--url", srv.URL},
		"truncated body":    {"-o", filepath.Join(dir, "truncated.gz"), "--url", truncated.URL},
	}
	for name, args := range tests {
		if _, stderr, err := execute(t, "", append([]string{"download"}, args...)...); err == nil {
			t.Errorf("%s: expected an error\n%s", name, stderr)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "truncated.gz")); !os.IsNotExist(err) {
		t.Error("partial download left behind")
	}
}

func TestUnzipErrors(t *testing.T) {
	dir := t.TempDir()
	gz := filepath.Join(dir, "jmdict.gz")
	notGzip := filepath.Join(dir, "plain.xml")
	truncated := filepath.Join(dir, "truncated.gz")
	data := sampleGzip(t)
	_ = os.WriteFile(gz, data, 0o644)
	_ = os.WriteFile(notGzip, []byte("<JMdict/>"), 0o644)
	_ = os.WriteFile(truncated, data[:len(data)/2], 0o644)

	tests := map[string][]string{
		"missing input":     {"-i", filepath.Join(dir, "none.gz"), "-o", filepath.Join(dir, "a.xml")},
		"not gzip":          {"-i", notGzip, "-o", filepath.Join(dir, "b.xml")},
		"truncated gzip":    {"-i", truncated, "-o", filepath.Join(dir, "c.xml")},
		"missing directory": {"-i", gz, "-o", filepath.Join(dir, "missing", "d.xml")},
	}
	for name, args := range tests {
		if _, stderr, err := execute(t, "", append([]string{"unzip"}, args...)...); err == nil {
			t.Errorf("%s: expected an error\n%s", name, stderr)
		}
	}
	for _, out := range []string{"a.xml", "b.xml", "c.xml"} {
		if _, err := os.Stat(filepath.Join(dir, out)); !os.IsNotExist(err) {
			t.Errorf("%s left behind", out)
		}
	}
}

func TestConvertErrors(t *testing.T) {
	dir := t.TempDir()
	xml := filepath.Join(dir, "jmdict.xml")
	badXML := filepath.Join(dir, "bad.xml")
	badGzip := filepath.Join(dir, "bad.gz")
	_ = os.WriteFile(xml, []byte("<JMdict/>"), 0o644)
	_ = os.WriteFile(badXML, []byte("<JMdict><entry>"), 0o644)
	_ = os.WriteFile(badGzip, []byte{0x1f, 0x8b, 0x00}, 0o644)

	tests := map[string][]string{
		"missing input":     {"-i", filepath.Join(dir, "none.xml"), "-o", filepath.Join(dir, "a.json")},
		"invalid XML":       {"-i", badXML, "-o", filepath.Join(dir, "b.json")},
		"corrupt gzip":      {"-i", badGzip, "-o", filepath.Join(dir, "c.json")},
		"missing directory": {"-i", xml, "-o", filepath.Join(dir, "missing", "d.json")},
	}
	for name, args := range tests {
		if _, stderr, err := execute(t, "", append([]string{"convert"}, args...)...); err == nil {
			t.Errorf("%s: expected an error\n%s", name, stderr)
		}
	}
	for _, out := range []string{"a.json", "b.json", "c.json"} {
		if _, err := os.Stat(filepath.Join(dir, out)); !os.IsNotExist(err) {
			t.Errorf("%s left behind", out)
		}
	}
}

func TestConvertPrettyAndExpand(t *testing.T) {
	xml, _ := os.ReadFile("../internal/jmdict/testdata/sample.xml")
	stdout, stderr, err := execute(t, string(xml), "convert", "-i", "-", "-o", "-", "--pretty", "--expand-entities")
	if err != nil {
		t.Fatalf("%v\n%s", err, stderr)
	}
	if !strings.HasPrefix(stdout, "[\n  {") || !strings.Contains(stdout, `"noun (common) (futsuumeishi)"`) {
		t.Errorf("output:\n%s", stdout)
	}
}

func TestConvertEmptyInput(t *testing.T) {
	stdout, stderr, err := execute(t, "", "convert", "-i", "-", "-o", "-")
	if err != nil || stdout != "[]\n" {
		t.Errorf("stdout = %q, err = %v\n%s", stdout, err, stderr)
	}
}

func TestLogFormatFromEnv(t *testing.T) {
	t.Setenv("LOG_FORMAT", "json")
	_, stderr, err := execute(t, "", "convert", "-i", "-", "-o", "-")
	if err != nil || !strings.HasPrefix(stderr, `{"timestamp"`) {
		t.Errorf("stderr = %q, err = %v", stderr, err)
	}
}

func TestUnknownFlagIsLogged(t *testing.T) {
	_, stderr, err := execute(t, "", "convert", "--no-such-flag")
	if err == nil || !strings.Contains(stderr, "level=ERROR") || !strings.Contains(stderr, "no-such-flag") {
		t.Errorf("err = %v, stderr = %s", err, stderr)
	}
}

// Execute exits the process, so it runs in a child copy of the test binary.
// The exit status is what makes a Cloud Run job execution fail.
func TestExecuteExitStatus(t *testing.T) {
	if args := os.Getenv("JMDICT_TO_JSON_EXECUTE"); args != "" {
		os.Args = append([]string{"jmdict-to-json"}, strings.Fields(args)...)
		Execute()
		os.Exit(0)
	}
	tests := map[string]int{
		"--help":                       0,
		"convert -i /nonexistent -o -": 1,
	}
	for args, want := range tests {
		c := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestExecuteExitStatus$")
		c.Env = append(os.Environ(), "JMDICT_TO_JSON_EXECUTE="+args)
		err := c.Run()
		got := 0
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			got = exitErr.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("%q: exit status %d, want %d", args, got, want)
		}
	}
}
