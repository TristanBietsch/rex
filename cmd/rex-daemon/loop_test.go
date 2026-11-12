package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func startLoopDaemon(t *testing.T, dir string) (c *client.Client, stop func()) {
	t.Helper()
	sock := filepath.Join(dir, "rex.sock")
	errc := make(chan error, 1)
	go func() {
		errc <- run([]string{"-socket", sock, "-state-dir", dir, "-tools", filepath.Join(dir, "tools.yaml")})
	}()
	require.Eventually(t, func() bool {
		var derr error
		c, derr = client.Dial(sock)
		return derr == nil
	}, 10*time.Second, 20*time.Millisecond)
	_, err := c.Hello(protocol.ClientTUI)
	require.NoError(t, err)
	return c, func() {
		require.NoError(t, c.Shutdown())
		select {
		case err := <-errc:
			require.NoError(t, err)
		case <-time.After(15 * time.Second):
			t.Fatal("daemon did not stop after Shutdown")
		}
		_ = c.Close()
	}
}

func isBadIntent(err error, msg string) bool {
	var ce *client.Error
	return errors.As(err, &ce) && ce.Code == protocol.ErrCodeBadIntent && ce.Message == msg
}

// With the loops flag off (the default) the daemon refuses the intents, reads
// no loop file and writes no loop state. Switched on, a loop starts through
// the normal spawn path, shows on its session, reaches a late client's
// Snapshot, and after a daemon restart it is paused.
func TestRun_LoopsEndToEnd(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("loop file modes are unix only")
	}
	dir, err := os.MkdirTemp("", "rexd")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	loopsDir, err := paths.LoopsDir()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(loopsDir, 0o700))
	// A test agent that reports why it waits (Claude's hooks), running a
	// plain shell: no real agent is ever started.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tools.yaml"), []byte(`tools:
  - id: hookbash
    name: "Hook test"
    category: self_hosted
    command: ["bash", "-c"]
    detect: {kind: hooks, format: claude}
    icon: "o"
    color: "#94A3B8"
    models:
      - {id: short, name: "Short", args: ["sleep 20"]}
`), 0o600))
	for name, tool := range map[string]string{"echoer": "hookbash", "plain": "echo"} {
		require.NoError(t, os.WriteFile(filepath.Join(loopsDir, name+".yaml"), []byte(
			"name: "+name+"\nkind: repeat\ntool: "+tool+"\nmodel: short\nprompt: hello\nuntil:\n  check: [\"true\"]\n"), 0o600))
	}
	cfg, err := paths.ConfigFile()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(cfg), 0o700))
	require.NoError(t, os.WriteFile(cfg, []byte("summary_enabled: false\n"), 0o600))

	c, stop := startLoopDaemon(t, dir)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Off: refused, inert.
	_, err = c.StartLoop(ctx, protocol.StartLoop{Name: "echoer", CWD: dir})
	require.True(t, isBadIntent(err, "off: enable loops in Settings → Features"), "%v", err)
	_, statErr := os.Stat(filepath.Join(dir, "loops"))
	require.True(t, os.IsNotExist(statErr), "no loop state while off")

	// On (the daemon follows config.yaml within a few seconds).
	require.NoError(t, os.WriteFile(cfg, []byte("summary_enabled: false\nloops: true\n"), 0o600))
	var l protocol.LoopSummary
	require.Eventually(t, func() bool {
		var serr error
		l, serr = c.StartLoop(ctx, protocol.StartLoop{Name: "echoer", CWD: dir})
		return serr == nil
	}, 20*time.Second, 200*time.Millisecond, "the daemon never saw loops switch on")
	require.Equal(t, protocol.LoopRunning, l.State)
	require.NotEmpty(t, l.SessionID)
	fi, err := os.Stat(filepath.Join(dir, "loops", l.ID+".json"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), fi.Mode().Perm())

	late, err := client.Dial(filepath.Join(dir, "rex.sock"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = late.Close() })
	snap, err := late.Hello(protocol.ClientTUI)
	require.NoError(t, err)
	require.Len(t, snap.Loops, 1)
	require.Equal(t, l.ID, snap.Loops[0].ID)
	var found bool
	for _, s := range snap.Sessions {
		if s.ID == l.SessionID {
			found = true
			require.Equal(t, l.ID, s.LoopID)
			require.Equal(t, 1, s.LoopIter)
		}
	}
	require.True(t, found, "the loop's session is on the board")

	// A tool that does not say why it waits is refused: a loop could answer
	// its permission prompts.
	_, err = c.StartLoop(ctx, protocol.StartLoop{Name: "plain", CWD: dir})
	require.Error(t, err)
	require.Contains(t, err.Error(), "loops need an agent that reports why it is waiting")

	// A loop file that does not exist is refused.
	_, err = c.StartLoop(ctx, protocol.StartLoop{Name: "nope", CWD: dir})
	require.Error(t, err)

	stop()

	// A restart leaves the loop paused.
	c2, stop2 := startLoopDaemon(t, dir)
	defer stop2()
	snap2, err := c2.Hello(protocol.ClientTUI)
	require.NoError(t, err)
	require.Len(t, snap2.Loops, 1)
	require.Equal(t, protocol.LoopPaused, snap2.Loops[0].State)
}
