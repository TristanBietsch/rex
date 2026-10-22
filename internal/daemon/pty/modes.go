package pty

import (
	"bytes"

	"github.com/hinshun/vt10x"
)

// ModePreamble renders the terminal modes a child has switched on as the
// escape sequences that switch them on. A client that subscribes without
// transcript replay never sees the child's startup sequences; feeding it this
// preamble first makes its emulator's mode flags (alt screen, mouse tracking,
// SGR mouse encoding, application cursor keys) match the child's, so it can
// decide how to deliver mouse-wheel input.
func ModePreamble(m vt10x.ModeFlag) []byte {
	var b bytes.Buffer
	if m&vt10x.ModeAltScreen != 0 {
		b.WriteString("\x1b[?1049h")
	}
	switch {
	case m&vt10x.ModeMouseMany != 0:
		b.WriteString("\x1b[?1003h")
	case m&vt10x.ModeMouseMotion != 0:
		b.WriteString("\x1b[?1002h")
	case m&vt10x.ModeMouseButton != 0:
		b.WriteString("\x1b[?1000h")
	case m&vt10x.ModeMouseX10 != 0:
		b.WriteString("\x1b[?9h")
	}
	if m&vt10x.ModeMouseSgr != 0 {
		b.WriteString("\x1b[?1006h")
	}
	if m&vt10x.ModeAppCursor != 0 {
		b.WriteString("\x1b[?1h")
	}
	return b.Bytes()
}
