package lifecycle

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLastLines(t *testing.T) {
	require.Equal(t, "c\nd\n", string(lastLines([]byte("a\nb\nc\nd\n"), 2)))
	require.Equal(t, "c\nd", string(lastLines([]byte("a\nb\nc\nd"), 2)))
	require.Equal(t, "a\nb\n", string(lastLines([]byte("a\nb\n"), 5)))
	require.Equal(t, "", string(lastLines(nil, 3)))
}

func TestTailFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.log")
	require.NoError(t, os.WriteFile(path, []byte("one\ntwo\nthree\n"), 0o644))
	var out bytes.Buffer
	require.NoError(t, tailFile(&out, path, 2, false))
	require.Equal(t, "two\nthree\n", out.String())

	require.Error(t, tailFile(&out, filepath.Join(t.TempDir(), "missing.log"), 2, false))
}
