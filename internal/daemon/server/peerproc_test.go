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
	"time"

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

// startOrphanInSession starts a session leader (a child of the test process,
// in a new session, as the daemon starts every agent) whose subshell
// backgrounds `sleep` and exits at once: the shell idiom `(cmd &)`. The
// sleep is re-parented away from the test process but stays in the
// leader's session. It returns the orphan's pid.
func startOrphanInSession(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("sh", "-c", "(sleep 30 & echo $!); exec sleep 30")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	out, err := cmd.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())
	line, err := bufio.NewReader(out).ReadString('\n')
	require.NoError(t, err)
	orphan, err := strconv.Atoi(strings.TrimSpace(line))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = syscall.Kill(orphan, syscall.SIGKILL)
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	require.Eventually(t, func() bool {
		under, err := descendsFrom(orphan, os.Getpid())
		return err == nil && !under
	}, 5*time.Second, 10*time.Millisecond, "the subshell exits and the sleep is re-parented out of the test's tree")
	return orphan
}

func TestSessionOf(t *testing.T) {
	sid, err := sessionOf(os.Getpid())
	require.NoError(t, err)
	require.Positive(t, sid)

	cmd := exec.Command("sleep", "30")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	sid, err = sessionOf(cmd.Process.Pid)
	require.NoError(t, err)
	require.Equal(t, cmd.Process.Pid, sid, "a setsid child leads its own session")
}

func TestSessionOf_GoneProcessFails(t *testing.T) {
	cmd := exec.Command("true")
	require.NoError(t, cmd.Run())
	_, err := sessionOf(cmd.Process.Pid)
	require.Error(t, err)
}

// A process that left the daemon's tree with `(cmd &)` still runs in the
// agent's session, so it still counts as inside a session.
func TestInSession_OrphanStaysInAgentSession(t *testing.T) {
	self := os.Getpid()
	orphan := startOrphanInSession(t)

	under, err := descendsFrom(orphan, self)
	require.NoError(t, err)
	require.False(t, under, "the orphan no longer descends from the daemon")

	in, err := inDaemonSession(orphan, self)
	require.NoError(t, err)
	require.True(t, in)
}

func TestInDaemonSession_OwnTerminalIsNot(t *testing.T) {
	if sid, err := sessionOf(os.Getpid()); err == nil && sid == os.Getpid() {
		t.Skip("the test binary leads its own session")
	}
	in, err := inDaemonSession(os.Getpid(), os.Getpid())
	require.NoError(t, err)
	require.False(t, in, "the daemon's own process is not inside a session")

	grandchild := startGrandchild(t) // same session as the test process
	in, err = inDaemonSession(grandchild, os.Getpid())
	require.NoError(t, err)
	require.False(t, in, "a process in the test's own session is not in an agent's")
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
