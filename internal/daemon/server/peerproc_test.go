//go:build linux || darwin

package server

import (
	"bufio"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

// startChild starts argv as a child of the test process and kills and reaps
// it when the test ends.
func startChild(t *testing.T, argv ...string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(argv[0], argv[1:]...) //nolint:gosec // G204: fixed test commands.
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	return cmd
}

// startGrandchild starts a shell that starts `sleep` in the background and
// returns the sleep's pid: a grandchild of the test process.
func startGrandchild(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("sh", "-c", "sleep 30 & echo $!; wait")
	out, err := cmd.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())
	line, err := bufio.NewReader(out).ReadString('\n')
	require.NoError(t, err)
	pid, err := strconv.Atoi(strings.TrimSpace(line))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	return pid
}

func TestParentPID_OfChildIsThisProcess(t *testing.T) {
	cmd := startChild(t, "sleep", "30")
	ppid, err := parentPID(cmd.Process.Pid)
	require.NoError(t, err)
	require.Equal(t, os.Getpid(), ppid)
}

func TestParentPID_GoneProcessFails(t *testing.T) {
	cmd := exec.Command("true")
	require.NoError(t, cmd.Run())
	_, err := parentPID(cmd.Process.Pid)
	require.Error(t, err)
}

func TestDescendsFrom(t *testing.T) {
	self := os.Getpid()
	grandchild := startGrandchild(t)

	cases := []struct {
		name string
		pid  int
		want bool
	}{
		{"grandchild", grandchild, true},
		{"itself is not its own descendant", self, false},
		{"its parent", os.Getppid(), false},
		{"init", 1, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := descendsFrom(tc.pid, self)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestDescendsFrom_GoneProcessFails(t *testing.T) {
	cmd := exec.Command("true")
	require.NoError(t, cmd.Run())
	_, err := descendsFrom(cmd.Process.Pid, os.Getpid())
	require.Error(t, err)
}

func TestPeerPID_IsTheDialingProcess(t *testing.T) {
	dir, err := os.MkdirTemp("", "rex-pp")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	l, err := net.Listen("unix", filepath.Join(dir, "s.sock"))
	require.NoError(t, err)
	t.Cleanup(func() { l.Close() })

	dialed, err := net.Dial("unix", filepath.Join(dir, "s.sock"))
	require.NoError(t, err)
	t.Cleanup(func() { dialed.Close() })
	conn, err := l.Accept()
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })

	pid, err := peerPID(conn.(*net.UnixConn))
	require.NoError(t, err)
	require.Equal(t, os.Getpid(), pid)
}
