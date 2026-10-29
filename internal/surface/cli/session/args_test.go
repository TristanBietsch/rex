package session

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// isolateDefaults points the default socket and config at a temp dir so a
// command that ignored -socket could never reach the live daemon.
func isolateDefaults(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_RUNTIME_DIR", dir)
}

// recordingDaemon serves one connection: a Snapshot of renameFixture on
// Hello, then every other intent is sent on the returned channel and passed
// to reply (when non-nil).
func recordingDaemon(t *testing.T, reply func(w *protocol.Writer, env protocol.Envelope)) (string, <-chan protocol.Envelope) {
	t.Helper()
	dir, err := os.MkdirTemp("", "rexcli")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "d.sock")
	ln, err := net.Listen("unix", sock)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	seen := make(chan protocol.Envelope, 8)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		r, w := protocol.NewReader(conn), protocol.NewWriter(conn)
		for {
			env, err := r.Read()
			if err != nil {
				return
			}
			if env.Type == protocol.IntentHello {
				_ = w.WriteEvent(protocol.EventSnapshot, env.ID, protocol.Snapshot{Sessions: renameFixture})
				continue
			}
			seen <- env
			if reply != nil {
				reply(w, env)
			}
		}
	}()
	return sock, seen
}

func nextIntent(t *testing.T, seen <-chan protocol.Envelope) protocol.Envelope {
	t.Helper()
	select {
	case env := <-seen:
		return env
	case <-time.After(2 * time.Second):
		t.Fatal("daemon saw no intent")
		return protocol.Envelope{}
	}
}

func exitCode(t *testing.T, err error) int {
	t.Helper()
	if err == nil {
		return core.ExitOK
	}
	var ee core.ExitError
	require.ErrorAs(t, err, &ee, "err=%v", err)
	return ee.Code
}

// Flags after the selector parse; extra or missing positionals are rejected
// before the daemon is dialed. Reaching the dial (exit 4 on a dead socket)
// proves the arguments were accepted.
func TestSessionCommandArity(t *testing.T) {
	isolateDefaults(t)
	dead := filepath.Join(t.TempDir(), "none.sock")
	cases := []struct {
		name string
		run  func([]string) error
		args []string
		want int
	}{
		{"attach flags after sel", RunAttach, []string{"demo", "-read-only", "-no-replay", "-socket", dead}, core.ExitDaemonUnreachable},
		{"attach extra", RunAttach, []string{"demo", "x", "-socket", dead}, core.ExitInvalidArgs},
		{"attach none", RunAttach, []string{"-socket", dead}, core.ExitInvalidArgs},
		{"send flags after sel", RunSend, []string{"demo", "-socket", dead}, core.ExitDaemonUnreachable},
		{"send extra", RunSend, []string{"demo", "text", "-socket", dead}, core.ExitInvalidArgs},
		{"wait flags after sel", RunWait, []string{"demo", "-until", "any", "-timeout", "1s", "-socket", dead}, core.ExitDaemonUnreachable},
		{"wait extra", RunWait, []string{"demo", "x", "-socket", dead}, core.ExitInvalidArgs},
		// A typo would never match and block forever; reject it before dialing.
		{"wait bad until", RunWait, []string{"demo", "-until", "dnoe", "-socket", dead}, core.ExitInvalidArgs},
		{"wait until crashed", RunWait, []string{"demo", "-until", "crashed", "-socket", dead}, core.ExitDaemonUnreachable},
		{"rm flags after sel", RunRm, []string{"demo", "-force", "-socket", dead}, core.ExitDaemonUnreachable},
		{"rm extra", RunRm, []string{"demo", "x", "-force", "-socket", dead}, core.ExitInvalidArgs},
		{"rename flags after", RunRename, []string{"demo", "new-slug", "-socket", dead}, core.ExitDaemonUnreachable},
		{"rename extra", RunRename, []string{"demo", "a", "b", "-socket", dead}, core.ExitInvalidArgs},
		{"rename one", RunRename, []string{"demo", "-socket", dead}, core.ExitInvalidArgs},
		{"done flags after sel", RunDone, []string{"demo", "-after", "2h", "-socket", dead}, core.ExitDaemonUnreachable},
		{"done extra", RunDone, []string{"demo", "x", "-socket", dead}, core.ExitInvalidArgs},
		{"undone flags after sel", RunUndone, []string{"demo", "-socket", dead}, core.ExitDaemonUnreachable},
		{"undone extra", RunUndone, []string{"demo", "x", "-socket", dead}, core.ExitInvalidArgs},
		{"archive flags after sel", RunArchive, []string{"demo", "-socket", dead}, core.ExitDaemonUnreachable},
		{"archive extra", RunArchive, []string{"demo", "x", "-socket", dead}, core.ExitInvalidArgs},
		{"ls stray positional", RunLs, []string{"x", "-json", "-socket", dead}, core.ExitInvalidArgs},
		{"ls flags", RunLs, []string{"-json", "-socket", dead}, core.ExitDaemonUnreachable},
		{"reply flags after text", RunReply, []string{"demo", "hi", "-raw", "-socket", dead}, core.ExitDaemonUnreachable},
		{"reply none", RunReply, []string{"-socket", dead}, core.ExitInvalidArgs},
		{"new flags after prompt", RunNew, []string{"hello rex", "--tool", "echo", "--slug", "demo", "-socket", dead}, core.ExitDaemonUnreachable},
		{"unknown flag after sel", RunAttach, []string{"demo", "-bogus", "-socket", dead}, core.ExitInvalidArgs},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.want, exitCode(t, c.run(c.args)))
		})
	}
}

func TestRunReply_FlagsAfterText(t *testing.T) {
	sock, seen := recordingDaemon(t, nil)
	require.NoError(t, RunReply([]string{"beta", "hi", "-raw", "-socket", sock}))
	env := nextIntent(t, seen)
	require.Equal(t, protocol.IntentSendInput, env.Type, "-raw sends input without a newline")
	var in protocol.SendInput
	require.NoError(t, json.Unmarshal(env.Data, &in))
	require.Equal(t, "hi", string(in.Bytes))
}

func TestRunReply_DoubleDashKeepsDashText(t *testing.T) {
	sock, seen := recordingDaemon(t, nil)
	require.NoError(t, RunReply([]string{"-socket", sock, "beta", "--", "-raw", "x"}))
	env := nextIntent(t, seen)
	require.Equal(t, protocol.IntentReply, env.Type)
	var r protocol.Reply
	require.NoError(t, json.Unmarshal(env.Data, &r))
	require.Equal(t, "-raw x", r.Text)
}
