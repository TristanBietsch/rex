// Package rexlog wires log/slog to a per-binary file under
// ~/.local/state/rex/. Both the daemon and the TUI write to files because
// stdout/stderr are reserved (the TUI owns the alt-screen, the daemon prints
// startup banners to stderr).
//
// Level is controlled by REX_LOG_LEVEL=debug|info|warn|error (default: info).
package rexlog

import (
	"fmt"
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

// openLogFile opens <stateDir>/<name>.log privately (see OpenPrivate). The
// default directory is rex's own and is created or tightened to 0700. A
// REX_LOG_DIR the user chose may be shared (even /tmp), so it is created
// 0700 when missing but never chmodded.
func openLogFile(name string) (*os.File, error) {
	dir, custom := logDir()
	if custom {
		if err := os.MkdirAll(dir, privateDirMode); err != nil {
			return nil, fmt.Errorf("create log dir: %w", err)
		}
	} else if err := PrivateDir(dir); err != nil {
		return nil, err
	}
	return OpenPrivate(filepath.Join(dir, name+".log"))
}

func stateDir() string {
	dir, _ := logDir()
	return dir
}

// logDir returns the log directory and whether the user chose it via
// REX_LOG_DIR.
func logDir() (dir string, custom bool) {
	if d := os.Getenv("REX_LOG_DIR"); d != "" {
		return d, true
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state", "rex"), false
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
