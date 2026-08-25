// Package logging configures structured logging using slog.
package logging

import (
	"log/slog"
	"os"
	"strings"
)

// New builds a slog.Logger. In production it emits JSON; in development it emits
// human-readable text. All output goes to stdout so the container runtime owns
// log collection. No log files are written.
func New(level string, production bool) *slog.Logger {
	opts := &slog.HandlerOptions{Level: parseLevel(level)}

	var handler slog.Handler
	if production {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}
	return slog.New(handler)
}

func parseLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
