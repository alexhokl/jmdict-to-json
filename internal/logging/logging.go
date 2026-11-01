// Package logging creates the loggers for the text and JSON log formats.
package logging

import (
	"fmt"
	"io"
	"log/slog"
)

// Log formats accepted by New.
const (
	FormatText = "text"
	FormatJSON = "json"
)

// New returns a logger writing to w in the given format.
func New(w io.Writer, format string) (*slog.Logger, error) {
	switch format {
	case FormatText:
		return slog.New(slog.NewTextHandler(w, nil)), nil
	case FormatJSON:
		return newJSON(w), nil
	default:
		return nil, fmt.Errorf("invalid log format %q, want %s or %s", format, FormatText, FormatJSON)
	}
}

// newJSON writes JSON in the shape Cloud Logging understands: severity,
// message and timestamp at the top level. Without the renaming every entry
// would be ingested at default severity and the alert filter on severity
// would never match.
//
// See https://cloud.google.com/logging/docs/structured-logging
func newJSON(w io.Writer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{
		ReplaceAttr: replaceAttrForGCP,
	}))
}

func replaceAttrForGCP(groups []string, attr slog.Attr) slog.Attr {
	if len(groups) > 0 {
		return attr
	}
	switch attr.Key {
	case slog.LevelKey:
		attr.Key = "severity"
		if lvl, ok := attr.Value.Any().(slog.Level); ok {
			attr.Value = slog.StringValue(severity(lvl))
		}
	case slog.MessageKey:
		attr.Key = "message"
	case slog.TimeKey:
		attr.Key = "timestamp"
	}
	return attr
}

func severity(l slog.Level) string {
	switch {
	case l >= slog.LevelError:
		return "ERROR"
	case l >= slog.LevelWarn:
		return "WARNING"
	case l >= slog.LevelInfo:
		return "INFO"
	default:
		return "DEBUG"
	}
}
