//go:build !windows

package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// hotTools is a fake agent: it prints its pid, a background tick stream
// (output that keeps flowing while the daemon hands over) and echoes each
// input line.
const hotTools = `tools:
  - id: echo
    models:
      - id: hot
        name: "Hot restart probe"
        args: ["stty -echo; echo PID:$$; ( i=0; while [ $i -lt 150 ]; do echo tick$i; i=$((i+1)); sleep 0.02; done ) & while read l; do echo got:$l; [ \"$l\" = pid ] && echo PID:$$; done"]
      - id: fds
        name: "Open descriptors probe"
        args: ["echo FDS:$(ls /dev/fd | tr '\\n' ,):END; sleep 30"]
`

// buildDaemon builds this package's binary into dir: a hot restart execs
// the daemon's own executable, which must be a real rex-daemon.
func buildDaemon(t *testing.T, dir string) string {
	t.Helper()
	bin := filepath.Join(dir, "rex-daemon")
	gobin := filepath.Join(runtime.GOROOT(), "bin", "go") //nolint:staticcheck // SA1019: the test builds on the machine it runs on; PATH is isolated
	cmd := exec.Command(gobin, "build", "-o", bin, ".")
	cmd.Env = append(append(os.Environ(), goBuildEnv...), "PATH="+os.Getenv("PATH")+string(os.PathListSeparator)+filepath.Dir(gobin))
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	return bin
}

func hello(t *testing.T, socket string) (*client.Client, *protocol.Snapshot) {
	t.Helper()
	var c *client.Client
	var err error
	require.Eventually(t, func() bool {
		c, err = client.Dial(socket)
		return err == nil
	}, 10*time.Second, 20*time.Millisecond)
	snap, err := c.Hello("hot-test")
	require.NoError(t, err)
	return c, snap
}

func transcript(t *testing.T, stateDir, id string) string {
	t.Helper()
	b, _ := os.ReadFile(filepath.Join(stateDir, "sessions", id, "transcript.log"))
	return string(b)
}

// A real hot restart: the daemon re-execs in place, the agent keeps its
// pid and PTY, output that flowed during the handoff is in the transcript
// exactly once, and input works through the new image.
func TestHotRestartKeepsSessionAlive(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs a daemon")
	}
	dir, err := os.MkdirTemp("", "rexhot")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	bin := buildDaemon(t, dir)
	stateDir := filepath.Join(dir, "state")
	socket := filepath.Join(dir, "rex.sock")
	tools := filepath.Join(dir, "tools.yaml")
	require.NoError(t, os.WriteFile(tools, []byte(hotTools), 0o600))

	logf, err := os.Create(filepath.Join(dir, "daemon.log"))
	require.NoError(t, err)
	defer logf.Close()
	d := exec.Command(bin, "-socket", socket, "-state-dir", stateDir, "-tools", tools)
	d.Stdout, d.Stderr = logf, logf
	require.NoError(t, d.Start())
	daemonPID := d.Process.Pid
	exited := make(chan error, 1)
	go func() { exited <- d.Wait() }()
	t.Cleanup(func() {
		_ = d.Process.Signal(syscall.SIGTERM)
		select {
		case <-exited:
		case <-time.After(10 * time.Second):
			_ = d.Process.Kill()
		}
		if t.Failed() {
			b, _ := os.ReadFile(logf.Name())
			t.Logf("daemon log:\n%s", b)
		}
	})

	c, snap := hello(t, socket)
	require.Equal(t, daemonPID, snap.DaemonPID)
	firstStart := snap.DaemonStartedAt
	require.False(t, firstStart.IsZero())
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	env, err := c.Do(ctx, protocol.IntentNewSession, protocol.NewSession{
		Slug: "hot", ToolID: "echo", ModelID: "hot", CWD: dir,
	})
	require.NoError(t, err)
	var ack protocol.Ack
	require.NoError(t, json.Unmarshal(env.Data, &ack))
	id := ack.SessionID
	require.NotEmpty(t, id)

	pidRe := regexp.MustCompile(`PID:(\d+)`)
	var childPID int
	require.Eventually(t, func() bool {
		m := pidRe.FindStringSubmatch(transcript(t, stateDir, id))
		if m == nil {
			return false
		}
		childPID, _ = strconv.Atoi(m[1])
		return true
	}, 10*time.Second, 20*time.Millisecond)
	require.NoError(t, c.SendInput(id, []byte("one\n")))
	require.Eventually(t, func() bool { return strings.Contains(transcript(t, stateDir, id), "got:one") }, 10*time.Second, 20*time.Millisecond)
	// Ticks are flowing.
	require.Eventually(t, func() bool { return strings.Contains(transcript(t, stateDir, id), "tick10") }, 10*time.Second, 10*time.Millisecond)

	fdsBefore := spawnFDs(t, c, ctx, stateDir, dir)

	// The restart: no reply on success, the connection just closes.
	_, err = c.Do(ctx, protocol.IntentRestart, protocol.Restart{Mode: protocol.RestartHot})
	require.Error(t, err, "a hot restart that worked never replies")
	_ = c.Close()

	c2, snap2 := hello(t, socket)
	defer c2.Close()
	require.Equal(t, daemonPID, snap2.DaemonPID, "same process: re-exec in place")
	require.True(t, snap2.DaemonStartedAt.After(firstStart), "a new image started")
	var sum *protocol.SessionSummary
	for i := range snap2.Sessions {
		if snap2.Sessions[i].ID == id {
			sum = &snap2.Sessions[i]
		}
	}
	require.NotNil(t, sum)
	require.NotEqual(t, protocol.StateCrashed, sum.State)
	require.NotEqual(t, protocol.StateFailed, sum.State)
	require.NoError(t, syscall.Kill(childPID, 0), "agent process still alive")

	require.NoError(t, c2.SendInput(id, []byte("pid\n")))
	require.Eventually(t, func() bool {
		return strings.Count(transcript(t, stateDir, id), "PID:"+strconv.Itoa(childPID)) == 2
	}, 10*time.Second, 20*time.Millisecond, "same agent answers through the new image")
	require.Eventually(t, func() bool { return strings.Contains(transcript(t, stateDir, id), "tick149") }, 15*time.Second, 20*time.Millisecond)

	all := transcript(t, stateDir, id)
	require.Equal(t, 1, strings.Count(all, "got:one"))
	for i := range 150 {
		require.Equal(t, 1, strings.Count(all, "tick"+strconv.Itoa(i)+"\r\n"), "tick%d exactly once", i)
	}
	// R20 review: a session spawned by the new image inherits no handed-over
	// descriptor (lock, other sessions' PTY masters): it sees the same open
	// fds as one spawned before the restart.
	require.Equal(t, fdsBefore, spawnFDs(t, c2, ctx, stateDir, dir), "no handed-over fd leaks into a new agent")

	// The handoff file is gone; meta still says live.
	matches, _ := filepath.Glob(filepath.Join(stateDir, "handoff-*"))
	require.Empty(t, matches)
	sess, err := state.LoadMeta(stateDir, id)
	require.NoError(t, err)
	require.NotEqual(t, protocol.StateCrashed, sess.State)
}

// A binary that cannot take the handoff (here: the -handoff-check probe
// fails because the executable was replaced by a script) is refused before
// anything is frozen; the daemon and its session go on.
func TestHotRestartRefusedByOldBinary(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs a daemon")
	}
	dir, err := os.MkdirTemp("", "rexhot")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	bin := buildDaemon(t, dir)
	stateDir := filepath.Join(dir, "state")
	socket := filepath.Join(dir, "rex.sock")
	tools := filepath.Join(dir, "tools.yaml")
	require.NoError(t, os.WriteFile(tools, []byte(hotTools), 0o600))
	d := exec.Command(bin, "-socket", socket, "-state-dir", stateDir, "-tools", tools)
	require.NoError(t, d.Start())
	exited := make(chan error, 1)
	go func() { exited <- d.Wait() }()
	t.Cleanup(func() {
		_ = d.Process.Signal(syscall.SIGTERM)
		select {
		case <-exited:
		case <-time.After(10 * time.Second):
			_ = d.Process.Kill()
		}
	})
	c, _ := hello(t, socket)
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	env, err := c.Do(ctx, protocol.IntentNewSession, protocol.NewSession{Slug: "hot", ToolID: "echo", ModelID: "hot", CWD: dir})
	require.NoError(t, err)
	var ack protocol.Ack
	require.NoError(t, json.Unmarshal(env.Data, &ack))
	require.Eventually(t, func() bool { return strings.Contains(transcript(t, stateDir, ack.SessionID), "PID:") }, 10*time.Second, 20*time.Millisecond)

	// "Install" an older build: anything that does not know -handoff-check.
	require.NoError(t, os.Remove(bin))
	require.NoError(t, os.WriteFile(bin, []byte("#!/bin/sh\necho 'flag provided but not defined: -handoff-check' >&2\nexit 2\n"), 0o700)) //nolint:gosec // G306: test script must be executable.

	_, err = c.Do(ctx, protocol.IntentRestart, protocol.Restart{Mode: protocol.RestartHot})
	var ce *client.Error
	require.ErrorAs(t, err, &ce)
	require.Equal(t, protocol.ErrCodeRestartRefused, ce.Code)
	require.Contains(t, ce.Message, "-cold")

	// Still the same daemon, input still flows.
	require.NoError(t, c.SendInput(ack.SessionID, []byte("after\n")))
	require.Eventually(t, func() bool { return strings.Contains(transcript(t, stateDir, ack.SessionID), "got:after") }, 10*time.Second, 20*time.Millisecond)
}

// R20 review: a tools.yaml broken between the start and the hot restart
// does not kill the new image (and with it every session): it serves with
// the built-in registry and says so.
func TestHotRestartSurvivesBrokenToolsYAML(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs a daemon")
	}
	dir, err := os.MkdirTemp("", "rexhot")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	bin := buildDaemon(t, dir)
	stateDir := filepath.Join(dir, "state")
	socket := filepath.Join(dir, "rex.sock")
	tools := filepath.Join(dir, "tools.yaml")
	require.NoError(t, os.WriteFile(tools, []byte(hotTools), 0o600))
	d := exec.Command(bin, "-socket", socket, "-state-dir", stateDir, "-tools", tools)
	require.NoError(t, d.Start())
	exited := make(chan error, 1)
	go func() { exited <- d.Wait() }()
	t.Cleanup(func() {
		_ = d.Process.Signal(syscall.SIGTERM)
		select {
		case <-exited:
		case <-time.After(10 * time.Second):
			_ = d.Process.Kill()
		}
	})
	c, _ := hello(t, socket)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	env, err := c.Do(ctx, protocol.IntentNewSession, protocol.NewSession{Slug: "hot", ToolID: "echo", ModelID: "hot", CWD: dir})
	require.NoError(t, err)
	var ack protocol.Ack
	require.NoError(t, json.Unmarshal(env.Data, &ack))
	require.Eventually(t, func() bool { return strings.Contains(transcript(t, stateDir, ack.SessionID), "PID:") }, 10*time.Second, 20*time.Millisecond)

	require.NoError(t, os.WriteFile(tools, []byte("tools: [unclosed\n"), 0o600))
	_, err = c.Do(ctx, protocol.IntentRestart, protocol.Restart{Mode: protocol.RestartHot})
	require.Error(t, err, "a hot restart that worked never replies")
	_ = c.Close()

	c2, snap := hello(t, socket)
	defer c2.Close()
	require.Equal(t, d.Process.Pid, snap.DaemonPID)
	require.NoError(t, c2.SendInput(ack.SessionID, []byte("after\n")))
	require.Eventually(t, func() bool { return strings.Contains(transcript(t, stateDir, ack.SessionID), "got:after") }, 10*time.Second, 20*time.Millisecond)
}

var fdsRe = regexp.MustCompile(`FDS:([0-9,]*):END`)

// spawnFDs starts the fds probe and returns the descriptors its shell had
// open.
func spawnFDs(t *testing.T, c *client.Client, ctx context.Context, stateDir, cwd string) string {
	t.Helper()
	env, err := c.Do(ctx, protocol.IntentNewSession, protocol.NewSession{Slug: "fds" + strconv.FormatInt(time.Now().UnixNano()%1e6, 10), ToolID: "echo", ModelID: "fds", CWD: cwd})
	require.NoError(t, err)
	var ack protocol.Ack
	require.NoError(t, json.Unmarshal(env.Data, &ack))
	var got string
	require.Eventually(t, func() bool {
		m := fdsRe.FindStringSubmatch(transcript(t, stateDir, ack.SessionID))
		if m == nil {
			return false
		}
		got = m[1]
		return true
	}, 10*time.Second, 20*time.Millisecond)
	return got
}
