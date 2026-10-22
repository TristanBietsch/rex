package pty

import (
	"testing"

	"github.com/hinshun/vt10x"
	"github.com/stretchr/testify/require"
)

func TestModePreamble(t *testing.T) {
	require.Empty(t, ModePreamble(0))
	require.Equal(t, "\x1b[?1049h\x1b[?1000h\x1b[?1006h",
		string(ModePreamble(vt10x.ModeAltScreen|vt10x.ModeMouseButton|vt10x.ModeMouseSgr)))
	require.Equal(t, "\x1b[?1003h\x1b[?1h", string(ModePreamble(vt10x.ModeMouseMany|vt10x.ModeAppCursor)))
	require.Equal(t, "\x1b[?9h", string(ModePreamble(vt10x.ModeMouseX10)))
}

func TestModePreamble_RoundTripsThroughEmulator(t *testing.T) {
	src := vt10x.New(vt10x.WithSize(40, 10))
	_, _ = src.Write([]byte("\x1b[?1049h\x1b[?1002h\x1b[?1006h\x1b[?1h"))
	dst := vt10x.New(vt10x.WithSize(40, 10))
	_, _ = dst.Write(ModePreamble(src.Mode()))
	mask := vt10x.ModeAltScreen | vt10x.ModeMouseMask | vt10x.ModeMouseSgr | vt10x.ModeAppCursor
	require.Equal(t, src.Mode()&mask, dst.Mode()&mask)
}
