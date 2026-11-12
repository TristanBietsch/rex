package tui

import (
	"fmt"
	"os"

	"github.com/tristanbietsch/rex/internal/catalog/theme"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
)

// themesDir is the user's theme directory, "" when the home directory is
// unknown.
func themesDir() string {
	dir, err := paths.ThemesDir()
	if err != nil {
		return ""
	}
	return dir
}

// configStamp summarizes config.yaml and the theme files: it changes when any
// of them is created, edited or removed. It stats paths, not inodes, so an
// editor that saves by rename is seen.
func configStamp(path, themeDir string) string {
	s := "-"
	if fi, err := os.Stat(path); err == nil {
		s = fmt.Sprintf("%d:%d", fi.Size(), fi.ModTime().UnixNano())
	}
	return s + "|" + theme.Stamp(themeDir)
}
