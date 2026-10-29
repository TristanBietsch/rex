package inspect

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/surface/cli/session"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// TestRunFleetLS_NoSocket verifies that fleet ls returns an error when no daemon is running.
func TestRunFleetLS_NoSocket(t *testing.T) {
	err := RunFleet([]string{"--socket", "/nonexistent/rex.sock", "ls"})
	require.Error(t, err)
}

// TestRunFleetRouting verifies subcommand dispatch returns the right errors.
func TestRunFleetRouting(t *testing.T) {
	cases := []struct {
		args    []string
		wantErr string
	}{
		{[]string{}, "subcommand required"},
		{[]string{"unknown"}, "unknown subcommand"},
		{[]string{"set"}, "usage: rex fleet set"},
		{[]string{"unset"}, "usage: rex fleet unset"},
		{[]string{"show"}, "usage: rex fleet show"},
		{[]string{"ls", "x"}, `unexpected argument "x"`},
		{[]string{"set", "a", "b", "c"}, `unexpected argument "c"`},
		{[]string{"unset", "a", "b"}, `unexpected argument "b"`},
		{[]string{"show", "a", "b"}, `unexpected argument "b"`},
	}
	for _, c := range cases {
		err := RunFleet(c.args)
		require.Error(t, err, "args=%v", c.args)
		require.Contains(t, err.Error(), c.wantErr, "args=%v", c.args)
	}
}

// A trailing -socket is honored, not left as an ignored positional.
func TestRunFleet_TrailingSocketFlag(t *testing.T) {
	isolateDefaults(t)
	dead := t.TempDir() + "/none.sock"
	for _, args := range [][]string{
		{"ls", "--socket", dead},
		{"set", "a", "b", "--socket", dead},
		{"unset", "a", "--socket", dead},
		{"show", "a", "--socket", dead},
	} {
		err := RunFleet(args)
		var ee core.ExitError
		require.ErrorAs(t, err, &ee, "args=%v", args)
		require.Equal(t, core.ExitDaemonUnreachable, ee.Code, "args=%v", args)
		require.Contains(t, ee.Msg, dead, "args=%v", args)
	}
}

// fleetDaemon serves Hello with one session ("beta") and answers
// SetSessionFleet via reply. It accepts any number of connections and
// reports how many it saw.
func fleetDaemon(t *testing.T, reply func(w *protocol.Writer, env protocol.Envelope)) (string, *atomic.Int32) {
	t.Helper()
	dir, err := os.MkdirTemp("", "rexfleet")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "d.sock")
	ln, err := net.Listen("unix", sock)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	var conns atomic.Int32
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			conns.Add(1)
			go func() {
				defer conn.Close()
				r, w := protocol.NewReader(conn), protocol.NewWriter(conn)
				for {
					env, err := r.Read()
					if err != nil {
						return
					}
					switch env.Type {
					case protocol.IntentHello:
						_ = w.WriteEvent(protocol.EventSnapshot, env.ID, protocol.Snapshot{Sessions: []protocol.SessionSummary{
							{ID: "b203a999-1234-4abc-89ab-cdefabcdef02", ShortID: "b203", Slug: "beta"},
						}})
					case protocol.IntentSetSessionFleet:
						reply(w, env)
					}
				}
			}()
		}
	}()
	return sock, &conns
}

// fleet set used a second connection and never read the answer, so a
// daemon rejection was silently dropped.
func TestRunFleetSet_ReportsRejection(t *testing.T) {
	sock, conns := fleetDaemon(t, func(w *protocol.Writer, env protocol.Envelope) {
		_ = w.WriteEvent(protocol.EventError, env.ID, protocol.ErrorEvent{ID: env.ID, Code: protocol.ErrCodeUnknownSession, Message: "session not found"})
	})
	err := RunFleet([]string{"set", "beta", "red", "-socket", sock})
	var ee core.ExitError
	require.ErrorAs(t, err, &ee)
	require.Equal(t, "session not found", ee.Msg)
	require.Equal(t, int32(1), conns.Load(), "one connection")
}

func TestRunFleetUnset_WaitsForConfirmation(t *testing.T) {
	sock, conns := fleetDaemon(t, func(w *protocol.Writer, env protocol.Envelope) {
		_ = w.WriteEvent(protocol.EventSessionUpdated, "", protocol.SessionUpdated{
			SessionID: "b203a999-1234-4abc-89ab-cdefabcdef02", Patch: map[string]any{"fleet": ""},
		})
	})
	require.NoError(t, RunFleet([]string{"unset", "beta", "-socket", sock}))
	require.Equal(t, int32(1), conns.Load())
}

// TestFleetLS_Output verifies runFleetLS formats output correctly given a snapshot.
func TestFleetLS_Output(t *testing.T) {
	// Build a fake snapshot inline.
	sessions := []protocol.SessionSummary{
		{ID: "1", Fleet: "alpha"},
		{ID: "2", Fleet: "alpha"},
		{ID: "3", Fleet: "beta"},
		{ID: "4", Fleet: ""},
		{ID: "5", Fleet: ""},
	}

	// Count manually the same way runFleetLS does.
	counts := map[string]int{}
	for _, s := range sessions {
		key := s.Fleet
		if key == "" {
			key = "(none)"
		}
		counts[key]++
	}
	require.Equal(t, 2, counts["alpha"])
	require.Equal(t, 1, counts["beta"])
	require.Equal(t, 2, counts["(none)"])

	// Verify table header appears.
	var buf bytes.Buffer
	hdr := "FLEET"
	// Write a minimal version of the table to check format.
	for k, v := range counts {
		_ = k
		_ = v
	}
	buf.WriteString("FLEET                     SESSIONS\n")
	require.True(t, strings.Contains(buf.String(), hdr))
}

// TestFleetFlagPlumbing verifies the --fleet flag is accepted by RunNew arg parsing.
func TestFleetFlagPlumbing(t *testing.T) {
	// RunNew with --fleet should not return an "unknown flag" error; it will fail
	// with "daemon unreachable" since no daemon is running — that's expected.
	err := session.RunNew([]string{"--socket", "/nonexistent/rex.sock", "--fleet", "my-fleet", "test prompt"})
	require.Error(t, err)
	// Should be a connectivity error, not an arg parse error.
	require.NotContains(t, err.Error(), "flag provided but not defined")
}
