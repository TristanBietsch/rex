package core

import (
	"os"
	"strconv"

	"golang.org/x/term"
)

// Terminal width bounds. Narrower than minWidth makes tables unreadable;
// wider than maxWidth makes lines too long to scan.
const (
	minWidth     = 40
	maxWidth     = 200
	defaultWidth = 80
)

// IsTerminal reports whether f is a terminal.
func IsTerminal(f *os.File) bool {
	return f != nil && term.IsTerminal(int(f.Fd()))
}

// StdoutWidth returns the width for rendering to stdout: the terminal width
// when stdout is a terminal, else $COLUMNS, else 80, clamped to [40, 200].
func StdoutWidth() int {
	return fileWidth(os.Stdout, os.Getenv)
}

func fileWidth(f *os.File, getenv func(string) string) int {
	if IsTerminal(f) {
		if w, _, err := term.GetSize(int(f.Fd())); err == nil && w > 0 {
			return clampWidth(w)
		}
	}
	if c, err := strconv.Atoi(getenv("COLUMNS")); err == nil && c > 0 {
		return clampWidth(c)
	}
	return defaultWidth
}

func clampWidth(w int) int { return min(max(w, minWidth), maxWidth) }

// EnableVT turns on VT escape processing for the console files that are
// terminals, so color renders on Windows. restore puts the previous modes
// back; call it before exit. It does nothing on other platforms.
func EnableVT(files ...*os.File) (restore func()) {
	var undo []func()
	for _, f := range files {
		if IsTerminal(f) {
			undo = append(undo, enableVT(f))
		}
	}
	return func() {
		for i := len(undo) - 1; i >= 0; i-- {
			undo[i]()
		}
	}
}

// StdinIsTerminal reports whether stdin is a terminal. A closed or missing
// stdin and a character device that is not a terminal (< /dev/null, < NUL)
// are not.
func StdinIsTerminal() bool { return IsTerminal(os.Stdin) }
