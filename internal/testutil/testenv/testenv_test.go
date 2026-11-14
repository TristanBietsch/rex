package testenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsolate_PointsEveryPathVarIntoDir(t *testing.T) {
	realHome, err := os.UserHomeDir()
	require.NoError(t, err)
	realBin := filepath.Join(realHome, ".local", "bin")

	for _, key := range []string{"HOME", "USERPROFILE", "XDG_CONFIG_HOME", "XDG_DATA_HOME",
		"XDG_STATE_HOME", "XDG_CACHE_HOME", "XDG_RUNTIME_DIR", "REX_LOG_DIR", "PATH"} {
		t.Setenv(key, os.Getenv(key)) // restored after the test
	}
	t.Setenv("OLLAMA_HOST", "example.invalid:1")
	t.Setenv("CLAUDECODE", "1")
	t.Setenv("REX_AGENT", "1")
	t.Setenv("PATH", realBin+string(os.PathListSeparator)+os.Getenv("PATH"))

	dir := t.TempDir()
	require.NoError(t, Isolate(dir))

	for _, key := range []string{"HOME", "USERPROFILE", "XDG_CONFIG_HOME", "XDG_DATA_HOME",
		"XDG_STATE_HOME", "XDG_CACHE_HOME", "XDG_RUNTIME_DIR", "REX_LOG_DIR"} {
		val := os.Getenv(key)
		require.Truef(t, strings.HasPrefix(val, dir), "%s=%q not under %q", key, val, dir)
		require.DirExists(t, val)
	}
	for _, key := range []string{"OLLAMA_HOST", "CLAUDECODE", "REX_AGENT"} {
		_, ok := os.LookupEnv(key)
		require.Falsef(t, ok, "%s must be unset", key)
	}
	for _, p := range filepath.SplitList(os.Getenv("PATH")) {
		require.NotEqual(t, filepath.Clean(realBin), filepath.Clean(p), "real ~/.local/bin left on PATH")
	}
}

func TestWithoutDir(t *testing.T) {
	sep := string(os.PathListSeparator)
	in := strings.Join([]string{"/a", "/home/u/.local/bin/", "/b", "/home/u/.local/bin"}, sep)
	require.Equal(t, "/a"+sep+"/b", withoutDir(in, "/home/u/.local/bin"))
}
