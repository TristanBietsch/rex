package core

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func swapStdin(t *testing.T, f *os.File) {
	t.Helper()
	saved := os.Stdin
	os.Stdin = f
	t.Cleanup(func() { os.Stdin = saved })
}

func TestStdinIsTerminal_Pipe(t *testing.T) {
	r, w, err := os.Pipe()
	require.NoError(t, err)
	defer r.Close()
	defer w.Close()
	swapStdin(t, r)
	require.False(t, StdinIsTerminal())
}

// A closed or missing stdin is not a terminal and must not panic.
func TestStdinIsTerminal_Closed(t *testing.T) {
	r, w, err := os.Pipe()
	require.NoError(t, err)
	require.NoError(t, w.Close())
	require.NoError(t, r.Close())
	swapStdin(t, r)
	require.False(t, StdinIsTerminal())
	swapStdin(t, nil)
	require.False(t, StdinIsTerminal())
}

func TestStdinIsTerminal_DevNull(t *testing.T) {
	f, err := os.Open(os.DevNull)
	require.NoError(t, err)
	defer f.Close()
	swapStdin(t, f)
	require.False(t, StdinIsTerminal(), "a character device that is not a terminal (< /dev/null, < NUL)")
}
