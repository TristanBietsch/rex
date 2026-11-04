// Package rexlog wires log/slog to a per-binary file in paths.LogDir
// (~/.local/state/rex/), or in $REX_LOG_DIR when set. Both the daemon and the
// TUI write to files because stdout/stderr are reserved (the TUI owns the
// alt-screen, the daemon prints startup banners to stderr).
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

	"github.com/tristanbietsch/rex/internal/runtime/paths"
)

var (
	initOnce sync.Once
	logFile  *os.File
)

// Init opens (or creates) the log file Path(name) and installs a slog
// default logger that writes to it. Safe to call multiple times — only the
// first call takes effect. When the file cannot be opened (no home
// directory, unwritable dir) it logs to stderr instead.
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

// Path returns the file Init(name) logs to: <dir>/<name>.log, where dir is
// $REX_LOG_DIR when set, else paths.LogDir. It fails only when REX_LOG_DIR is
// unset and the home directory is unknown.
func Path(name string) (string, error) {
	dir, _, err := logDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name+".log"), nil
}

// openLogFile opens Path(name) privately (see OpenPrivate). The default
// directory is rex's own and is created or tightened to 0700. A REX_LOG_DIR
// the user chose may be shared (even /tmp), so it is created 0700 when
// missing but never chmodded.
func openLogFile(name string) (*os.File, error) {
	dir, custom, err := logDir()
	if err != nil {
		return nil, err
	}
	if custom {
		if err := os.MkdirAll(dir, privateDirMode); err != nil {
			return nil, fmt.Errorf("create log dir: %w", err)
		}
	} else if err := PrivateDir(dir); err != nil {
		return nil, err
	}
	return OpenPrivate(filepath.Join(dir, name+".log"))
}

// logDir returns the log directory and whether the user chose it via
// REX_LOG_DIR.
func logDir() (dir string, custom bool, err error) {
	if d := os.Getenv("REX_LOG_DIR"); d != "" {
		return d, true, nil
	}
	dir, err = paths.LogDir()
	if err != nil {
		return "", false, fmt.Errorf("log dir: %w", err)
	}
	return dir, false, nil
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
