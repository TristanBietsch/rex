// Package rexlog wires log/slog to a per-binary file under
// ~/.local/state/rex/. Both the daemon and the TUI write to files because
// stdout/stderr are reserved (the TUI owns the alt-screen, the daemon prints
// startup banners to stderr).
//
// Level is controlled by REX_LOG_LEVEL=debug|info|warn|error (default: info).
package rexlog

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var (
	initOnce sync.Once
	logFile  *os.File
)

// Init opens (or creates) ~/.local/state/rex/<name>.log and installs a
// slog default logger that writes to it. Safe to call multiple times — only
// the first call takes effect.
func Init(name string) {
	initOnce.Do(func() {
		path, err := openLogFile(name)
		if err != nil {
			// Fall back to stderr so we never lose logs entirely.
			slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, handlerOpts())))
			slog.Warn("rexlog: file open failed, logging to stderr", "err", err)
			return
		}
		logFile = path
		slog.SetDefault(slog.New(slog.NewTextHandler(io.Writer(path), handlerOpts())))
		slog.Info("rexlog: initialized", "path", path.Name(), "binary", name)
	})
}

// Close flushes and closes the log file. Optional but tidy on shutdown.
func Close() {
	if logFile != nil {
		_ = logFile.Sync()
		_ = logFile.Close()
		logFile = nil
	}
}

func openLogFile(name string) (*os.File, error) {
	dir := stateDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return os.OpenFile(
		filepath.Join(dir, name+".log"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND,
		0o644,
	)
}

func stateDir() string {
	if d := os.Getenv("REX_LOG_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state", "rex")
}

func handlerOpts() *slog.HandlerOptions {
	return &slog.HandlerOptions{Level: levelFromEnv()}
}

func levelFromEnv() slog.Level {
	switch strings.ToLower(os.Getenv("REX_LOG_LEVEL")) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error", "err":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
