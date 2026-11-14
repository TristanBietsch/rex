package core

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFileSize_NotATerminal(t *testing.T) {
	r, w, err := os.Pipe()
	require.NoError(t, err)
	defer r.Close()
	defer w.Close()
	c, rows := fileSize(w)
	require.Zero(t, c)
	require.Zero(t, rows)
	c, rows = fileSize(nil)
	require.Zero(t, c)
	require.Zero(t, rows)
}

func TestStdoutSize_BothOrNeither(t *testing.T) {
	c, r := StdoutSize() // 0, 0 under `go test`; the real size in a terminal
	require.Equal(t, c == 0, r == 0)
}
