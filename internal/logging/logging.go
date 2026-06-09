// Package logging provides a structured logger for ShardForgeDB.
// It wraps the standard library log/slog package.
package logging

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

// New returns a *slog.Logger configured with the given level and format.
//
// level is one of: "debug", "info", "warn", "error" (case-insensitive).
// format is one of: "json", "text".
// An unrecognised level defaults to Info; an unrecognised format defaults to JSON.
func New(level, format string) *slog.Logger {
	return NewWithWriter(os.Stdout, level, format)
}

// NewWithWriter is like New but writes to w. Useful in tests.
func NewWithWriter(w io.Writer, level, format string) *slog.Logger {
	opts := &slog.HandlerOptions{Level: parseLevel(level)}

	var handler slog.Handler
	if strings.EqualFold(format, "text") {
		handler = slog.NewTextHandler(w, opts)
	} else {
		handler = slog.NewJSONHandler(w, opts)
	}

	return slog.New(handler)
}

// parseLevel converts a level string to a slog.Level value.
func parseLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
