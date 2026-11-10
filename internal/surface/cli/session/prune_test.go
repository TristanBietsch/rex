package session

import (
	"bytes"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

var pruneSessions = []protocol.SessionSummary{
	{ID: "11111111-1234-4abc-89ab-cdefabcdef01", ShortID: "1111", Slug: "done-one", State: protocol.StateDone},
	{ID: "22222222-1234-4abc-89ab-cdefabcdef02", ShortID: "2222", Slug: "failed-one", State: protocol.StateFailed},
	{ID: "33333333-1234-4abc-89ab-cdefabcdef03", ShortID: "3333", Slug: "busy-marked", State: protocol.StateWorking, RetireAt: time.Now().Add(time.Hour)},
	{ID: "44444444-1234-4abc-89ab-cdefabcdef04", ShortID: "4444", Slug: "live", State: protocol.StateNeedsInput},
}

// pruneDaemon serves Hello with pruneSessions and records Delete intents,
// acknowledging them when acks is true (a daemon that predates acks sends
// no daemon_version and no Ack).
type pruneDaemon struct {
	sock    string
	mu      sync.Mutex
	deleted []string
}

func (d *pruneDaemon) Deleted() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.deleted...)
}

func startPruneDaemon(t *testing.T, acks bool) *pruneDaemon {
	t.Helper()
	dir, err := os.MkdirTemp("", "rexprune")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	d := &pruneDaemon{sock: filepath.Join(dir, "d.sock")}
	ln, err := net.Listen("unix", d.sock)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
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
			switch env.Type {
			case protocol.IntentHello:
				snap := protocol.Snapshot{Sessions: pruneSessions}
				if acks {
					snap.DaemonVersion = "v1"
				}
				_ = w.WriteEvent(protocol.EventSnapshot, env.ID, snap)
			case protocol.IntentDelete:
				var del protocol.Delete
				if json.Unmarshal(env.Data, &del) != nil {
					continue
				}
				d.mu.Lock()
				d.deleted = append(d.deleted, del.SessionID)
				d.mu.Unlock()
				if acks && env.ID != "" {
					_ = w.WriteEvent(protocol.EventAck, env.ID, protocol.Ack{Intent: protocol.IntentDelete, SessionID: del.SessionID})
				}
			}
		}
	}()
	return d
}

// withPruneIO swaps RunPrune's terminal for buffers.
func withPruneIO(t *testing.T, stdin string, interactive bool) (*bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	saved := pruneIO
	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	pruneIO.in, pruneIO.out, pruneIO.errOut = strings.NewReader(stdin), out, errOut
	pruneIO.interactive = func() bool { return interactive }
	t.Cleanup(func() { pruneIO = saved })
	return out, errOut
}

var wantPruned = []string{pruneSessions[0].ID, pruneSessions[1].ID}

func exitCode(t *testing.T, err error) int {
	t.Helper()
	var ec core.ExitCoder
	require.True(t, errors.As(err, &ec), "error %v has no exit code", err)
	return ec.ExitCode()
}

func TestRunPrune_DryRunDeletesNothing(t *testing.T) {
	d := startPruneDaemon(t, true)
	out, _ := withPruneIO(t, "", false)
	require.NoError(t, RunPrune([]string{"-socket", d.sock, "-dry-run"}))
	require.Empty(t, d.Deleted())
	require.Contains(t, out.String(), "1111\tdone-one\tdone")
	require.Contains(t, out.String(), "2222\tfailed-one\tfailed")
	require.NotContains(t, out.String(), "busy-marked")
	require.Contains(t, out.String(), "would clear 2 (1 still working kept)")
}

func TestRunPrune_RefusesWithoutYesWhenNotInteractive(t *testing.T) {
	d := startPruneDaemon(t, true)
	withPruneIO(t, "", false)
	err := RunPrune([]string{"-socket", d.sock})
	require.Error(t, err)
	require.Equal(t, core.ExitOperationRefused, exitCode(t, err))
	require.Empty(t, d.Deleted())
}

func TestRunPrune_AsksAndAborts(t *testing.T) {
	d := startPruneDaemon(t, true)
	_, errOut := withPruneIO(t, "n\n", true)
	err := RunPrune([]string{"-socket", d.sock})
	require.Error(t, err)
	require.Contains(t, errOut.String(), "clear 2 completed session(s)?")
	require.Empty(t, d.Deleted())
}

func TestRunPrune_AsksAndDeletes(t *testing.T) {
	d := startPruneDaemon(t, true)
	out, _ := withPruneIO(t, "y\n", true)
	require.NoError(t, RunPrune([]string{"-socket", d.sock}))
	require.Equal(t, wantPruned, d.Deleted())
	require.Contains(t, out.String(), "cleared 2 (1 still working kept)")
}

func TestRunPrune_YesDeletesWithAcks(t *testing.T) {
	d := startPruneDaemon(t, true)
	out, _ := withPruneIO(t, "", false)
	require.NoError(t, RunPrune([]string{"-socket", d.sock, "-y"}))
	require.Equal(t, wantPruned, d.Deleted())
	require.Contains(t, out.String(), "cleared 2")
}

func TestRunPrune_OlderDaemonWithoutAcks(t *testing.T) {
	d := startPruneDaemon(t, false)
	withPruneIO(t, "", false)
	require.NoError(t, RunPrune([]string{"-socket", d.sock, "-yes"}))
	require.Eventually(t, func() bool { return len(d.Deleted()) == 2 }, 2*time.Second, 10*time.Millisecond)
	require.Equal(t, wantPruned, d.Deleted())
}

func TestRunPrune_RejectsArguments(t *testing.T) {
	err := RunPrune([]string{"some-session"})
	require.Error(t, err)
	require.Equal(t, core.ExitInvalidArgs, exitCode(t, err))
}
