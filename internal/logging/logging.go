// Package logging configures Foliarz's file logger.
package logging

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

// Init opens the log file in append mode and installs a text slog handler as
// the default logger. The caller owns the returned file and should close it.
func Init(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create log directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open log file: %w", err)
	}
	handler := slog.NewTextHandler(file, &slog.HandlerOptions{Level: slog.LevelInfo})
	slog.SetDefault(slog.New(handler))
	return file, nil
}
