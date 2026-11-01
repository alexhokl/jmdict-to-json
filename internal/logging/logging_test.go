package logging

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestJSONUsesCloudLoggingKeys(t *testing.T) {
	var buf bytes.Buffer
	logger, err := New(&buf, FormatJSON)
	if err != nil {
		t.Fatal(err)
	}
	logger.Error("failed", "entries", 3)

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["severity"] != "ERROR" || got["message"] != "failed" || got["timestamp"] == nil || got["entries"] != 3.0 {
		t.Errorf("entry = %v", got)
	}
	for _, key := range []string{"level", "msg", "time"} {
		if _, ok := got[key]; ok {
			t.Errorf("entry has key %q: %v", key, got)
		}
	}
}

func TestNewRejectsUnknownFormat(t *testing.T) {
	if _, err := New(&bytes.Buffer{}, "xml"); err == nil {
		t.Error("expected an error")
	}
}

func TestText(t *testing.T) {
	var buf bytes.Buffer
	logger, err := New(&buf, FormatText)
	if err != nil {
		t.Fatal(err)
	}
	logger.Info("converted", "entries", 3)
	if got := buf.String(); !strings.Contains(got, "level=INFO msg=converted entries=3") {
		t.Errorf("log = %q", got)
	}
}

func TestSeverity(t *testing.T) {
	tests := map[slog.Level]string{
		slog.LevelDebug:     "DEBUG",
		slog.LevelInfo:      "INFO",
		slog.LevelInfo + 2:  "INFO",
		slog.LevelWarn:      "WARNING",
		slog.LevelError:     "ERROR",
		slog.LevelError + 4: "ERROR",
	}
	for level, want := range tests {
		if got := severity(level); got != want {
			t.Errorf("severity(%v) = %q, want %q", level, got, want)
		}
	}
}

func TestReplaceAttrKeepsGroupedKeys(t *testing.T) {
	var buf bytes.Buffer
	logger, _ := New(&buf, FormatJSON)
	logger.WithGroup("g").Info("m", "level", "x", "msg", "y")
	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	g, _ := got["g"].(map[string]any)
	if g["level"] != "x" || g["msg"] != "y" {
		t.Errorf("grouped attributes renamed: %v", got)
	}
}
