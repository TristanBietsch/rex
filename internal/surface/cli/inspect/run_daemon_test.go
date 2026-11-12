package inspect

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/testutil"
	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func runIn(t *testing.T, format string, fn func() error) (string, error) {
	t.Helper()
	p, out := viewPrinter(t, format, 80, false)
	core.SetOut(p)
	t.Cleanup(func() { core.SetOut(nil) })
	err := fn()
	return out.String(), err
}

// digest, stats and fleet against a real daemon with one finished session.
func TestRunInspectCommands_RealDaemon(t *testing.T) {
	dir := t.TempDir()
	d := testutil.StartDaemon(t)
	t.Cleanup(d.Close)
	sock := []string{"-socket", d.Socket}

	c, err := client.Dial(d.Socket)
	require.NoError(t, err)
	defer c.Close()
	_, err = c.Hello("inspect-test")
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	env, err := c.Do(ctx, protocol.IntentNewSession, protocol.NewSession{ToolID: "echo", ModelID: "short", Slug: "one", CWD: dir})
	require.NoError(t, err)
	var ack protocol.Ack
	require.NoError(t, json.Unmarshal(env.Data, &ack))
	require.Eventually(t, func() bool {
		sc, err := client.Dial(d.Socket)
		require.NoError(t, err)
		defer sc.Close()
		snap, err := sc.Hello("inspect-poll")
		require.NoError(t, err)
		for _, s := range snap.Sessions {
			if s.ID == ack.SessionID {
				return s.State == protocol.StateDone
			}
		}
		return false
	}, 15*time.Second, 100*time.Millisecond)

	out, err := runIn(t, "agent", func() error { return RunStats(sock) })
	require.NoError(t, err)
	require.Contains(t, out, "ok cmd=stats sessions=1 done=1 failed=0 other=0")

	out, err = runIn(t, "agent", func() error { return RunDigest(sock) })
	require.NoError(t, err)
	require.Contains(t, out, "session id=")
	require.Contains(t, out, "ok cmd=digest count=1 date=")

	out, err = runIn(t, "txt", func() error { return RunFleet(append(sock, "ls")) })
	require.NoError(t, err)
	require.Contains(t, out, "(none)")

	out, err = runIn(t, "txt", func() error { return RunFleet(append(sock, "set", "one", "alpha")) })
	require.NoError(t, err)
	require.Contains(t, out, `set fleet "alpha" on session `)
	require.Eventually(t, func() bool {
		o, err := runIn(t, "agent", func() error { return RunFleet(append(sock, "show", "alpha")) })
		return err == nil && len(o) > 0 && o != "" && containsLine(o, "session ")
	}, 5*time.Second, 100*time.Millisecond)

	out, err = runIn(t, "agent", func() error { return RunFleet(append(sock, "unset", "one")) })
	require.NoError(t, err)
	require.Contains(t, out, "ok cmd=fleet id=")
	out, err = runIn(t, "txt", func() error { return RunFleet(append(sock, "show", "nope")) })
	require.NoError(t, err)
	require.Equal(t, "no sessions in fleet \"nope\"\n", out)

	out, err = runIn(t, "json", func() error { return RunFleet(append(sock, "ls")) })
	require.NoError(t, err)
	require.Contains(t, out, `"data":[{"fleet":`)
}

func containsLine(s, prefix string) bool {
	for _, l := range splitLines(s) {
		if len(l) >= len(prefix) && l[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := range s {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return out
}

// log and render print content: json and md are refused (exit 5).
func TestContentCommandsRefuseStructuredFormats(t *testing.T) {
	for _, run := range []func([]string) error{RunLog, RunRender} {
		for _, f := range []string{"json", "md"} {
			err := run([]string{"-o", f, "x"})
			var ec core.ExitCoder
			require.True(t, errors.As(err, &ec), f)
			require.Equal(t, core.ExitInvalidArgs, ec.ExitCode(), f)
		}
	}
}
