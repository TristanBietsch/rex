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
