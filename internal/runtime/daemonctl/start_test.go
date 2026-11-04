package daemonctl

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// startFake runs Start's spawn path with this test binary as the daemon.
func startFake(t *testing.T, mode, socket string, logf *os.File) (*StartResult, error) {
	t.Helper()
	exe, err := os.Executable()
	require.NoError(t, err)
	t.Setenv(fakeModeEnv, mode)
	return start(exe, socket, logf)
}

// Start passes -socket explicitly, so a TUI or CLI using a non-default
// socket gets a daemon listening there.
func TestStart_PassesSocketFlag(t *testing.T) {
	sock := socketIn(t)
	logPath := filepath.Join(shortTempDir(t), "daemon.log")
	logf, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	require.NoError(t, err)
	defer logf.Close()

	res, err := startFake(t, "serve", sock, logf)
	require.NoError(t, err)
	require.NotZero(t, res.PID)
	t.Cleanup(func() { _ = ShutdownDaemon(bg(), sock) })

	logged, err := os.ReadFile(logPath)
	require.NoError(t, err)
	require.Contains(t, string(logged), fmt.Sprintf("fake-daemon: args %q", []string{"-socket", sock}))
	require.NoError(t, ShutdownDaemon(bg(), sock))
}

// A daemon that dies before its socket comes up is reported at once with
// its exit status and the end of its log, not after the full StartTimeout.
func TestStart_ReportsEarlyExit(t *testing.T) {
	logPath := filepath.Join(shortTempDir(t), "daemon.log")
	logf, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	require.NoError(t, err)
	defer logf.Close()

	start := time.Now()
	_, err = startFake(t, "exit", socketIn(t), logf)
	require.ErrorIs(t, err, ErrExitedEarly)
	require.ErrorContains(t, err, "exit status 3")
	require.ErrorContains(t, err, "config broken")
	require.NotContains(t, err.Error(), "\n")
	require.Less(t, time.Since(start), StartTimeout/2)

	if runtime.GOOS != "windows" {
		info, err := os.Stat(logPath)
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "Start tightens the daemon log")
	}
}

// sun_path is 104 bytes on macOS and 108 on Linux; a longer path fails
// deep inside bind with "invalid argument".
func TestValidateSocketPath(t *testing.T) {
	require.NoError(t, ValidateSocketPath("/tmp/rex.sock"))
	err := ValidateSocketPath("/tmp/" + strings.Repeat("d", 200) + "/rex.sock")
	require.ErrorContains(t, err, "too long")
	require.ErrorContains(t, err, "XDG_RUNTIME_DIR")

	_, err = Start("/tmp/"+strings.Repeat("d", 200)+"/rex.sock", nil)
	require.ErrorContains(t, err, "too long", "Start validates before spawning")
}

func TestMaxSocketPathLen(t *testing.T) {
	for goos, want := range map[string]int{"darwin": 104, "freebsd": 104, "linux": 108, "windows": 108} {
		require.Equal(t, want, maxSocketPathLen(goos), goos)
	}
}

func TestLogTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log")
	body := strings.Repeat("x", 5000) + "\nfirst\n\nsecond\x1b[31m\nthird\n"
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()
	require.Equal(t, "first | second[31m | third", logTail(f))
	require.Empty(t, logTail(nil))
}
