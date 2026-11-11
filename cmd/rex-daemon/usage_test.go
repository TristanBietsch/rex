package main

import (
	"context"
	"encoding/json"
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

// withStdin runs fn with os.Stdin reading from in.
func withStdin(t *testing.T, in string, fn func()) {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	_, err = w.WriteString(in)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	saved := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = saved; _ = r.Close() }()
	fn()
}

// TestRun_UsageEndToEnd runs the whole daemon with usage on (the default):
// a status-line tap process (runTap) reports a Claude-shaped sample for a
// session, a UsageQuery then returns its cost, a late TUI gets the quota in
// its Snapshot, the ledger is a 0600 file, and switching usage off in
// config.yaml turns the query into "usage is off".
func TestRun_UsageEndToEnd(t *testing.T) {
	dir, err := os.MkdirTemp("", "rexd")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "rex.sock")
	errc := make(chan error, 1)
	go func() { errc <- run([]string{"-socket", sock, "-state-dir", dir, "-tools", ""}) }()

	var c *client.Client
	require.Eventually(t, func() bool {
		var derr error
		c, derr = client.Dial(sock)
		return derr == nil
	}, 10*time.Second, 20*time.Millisecond)
	t.Cleanup(func() { _ = c.Close() })
	_, err = c.Hello("usage-test")
	require.NoError(t, err)

	env, err := c.Do(context.Background(), protocol.IntentNewSession, protocol.NewSession{
		ToolID: "echo", ModelID: "short", Slug: "billing", CWD: dir,
	})
	require.NoError(t, err)
	var ack protocol.Ack
	require.NoError(t, json.Unmarshal(env.Data, &ack))

	// The tap, as Claude would run it: its stdin is the status-line JSON.
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir()) // no user status line: the tap prints nothing
	statusJSON := `{"session_id":"c1","model":{"id":"claude-test-x"},"cost":{"total_cost_usd":3.25},` +
		`"rate_limits":{"five_hour":{"used_percentage":42,"resets_at":1900000000}}}`
	var code int
	withStdin(t, statusJSON, func() {
		saved := os.Args
		defer func() { os.Args = saved }()
		code = runTap([]string{"--session", ack.SessionID, "--socket", sock})
	})
	require.Zero(t, code)

	var rep protocol.UsageReport
	require.Eventually(t, func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var qerr error
		rep, qerr = c.UsageQuery(ctx, protocol.UsageQuery{Range: "today"})
		return qerr == nil && rep.Total.CostUSD != nil
	}, 10*time.Second, 50*time.Millisecond, "the tap's cost never reached the daemon")
	require.InDelta(t, 3.25, *rep.Total.CostUSD, 1e-9)
	require.Equal(t, "echo", rep.Tools[0].Tool)
	require.Equal(t, "claude-test-x", rep.Tools[0].Models[0].Model)
	require.Len(t, rep.Quotas, 1)
	require.Equal(t, "5h", rep.Quotas[0].Window)

	late, err := client.Dial(sock)
	require.NoError(t, err)
	t.Cleanup(func() { _ = late.Close() })
	snap, err := late.Hello(protocol.ClientTUI)
	require.NoError(t, err)
	require.NotNil(t, snap.Usage, "a late client gets the current quotas")
	require.True(t, snap.Usage.Enabled)
	require.Len(t, snap.Usage.Quotas, 1)

	// The ledger appears when the session's end is written.
	ledger := filepath.Join(dir, paths.UsageLedgerFileName)
	require.Eventually(t, func() bool { _, err := os.Stat(ledger); return err == nil }, 10*time.Second, 50*time.Millisecond)
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(ledger)
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o600), fi.Mode().Perm())
	}

	// Switching usage off (the daemon polls config.yaml every 5 s).
	cfg, err := paths.ConfigFile()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(cfg), 0o700))
	require.NoError(t, os.WriteFile(cfg, []byte("usage: false\n"), 0o600))
	require.Eventually(t, func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, qerr := c.UsageQuery(ctx, protocol.UsageQuery{})
		var ce *client.Error
		return errors.As(qerr, &ce) && ce.Message == "usage is off"
	}, 20*time.Second, 200*time.Millisecond, "usage did not switch off")

	require.NoError(t, late.Shutdown())
	select {
	case err := <-errc:
		require.NoError(t, err)
	case <-time.After(15 * time.Second):
		t.Fatal("daemon did not stop after Shutdown")
	}
}

// A flag the tap cannot parse only means nothing is sent; stdin is still
// read and the process still exits 0 with no user status line.
func TestRunTap_BadFlagSendsNothing(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	var code int
	withStdin(t, `{"cost":{"total_cost_usd":1}}`, func() { code = runTap([]string{"--nope"}) })
	require.Zero(t, code)
}
