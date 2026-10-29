package daemonctl

import (
	"os"
	"path/filepath"
)

// DefaultStateDir is where session data and transcripts are stored.
func DefaultStateDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "rex")
}

// DefaultToolsPath is the user tools.yaml override location.
func DefaultToolsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "rex", "tools.yaml")
}

// LogPath is the rex-daemon stderr log file used by rex daemon start.
func LogPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state", "rex", "daemon.log")
}

// OpenLog opens LogPath for appending, owner-only: rex-daemon's stderr
// records session ids, paths and errors. The dir is rex's own, so an
// existing dir and log (0755/0644 from older builds) are tightened too.
// Tightening is best-effort: failing it must not cost the log.
func OpenLog() (*os.File, error) {
	path := LogPath()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	_ = os.Chmod(dir, 0o700)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	_ = f.Chmod(0o600)
	return f, nil
}
