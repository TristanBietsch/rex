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

// pruneDaemon serves every connection: Hello gets a snapshot (from
// snapshot, given how many Hellos came before), Delete is recorded and
// answered by reply. A daemon that predates acks (acks false) sends no
// daemon_version and never answers a Delete.
type pruneDaemon struct {
	sock     string
	acks     bool
	snapshot func(hellos int) []protocol.SessionSummary
	reply    func(w *protocol.Writer, env protocol.Envelope, id string) // nil: Ack

	mu      sync.Mutex
	hellos  int
	deleted []string
}

func (d *pruneDaemon) Deleted() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.deleted...)
}

func startPruneDaemon(t *testing.T, acks bool) *pruneDaemon {
	t.Helper()
	return startPruneDaemonWith(t, &pruneDaemon{acks: acks})
}

func startPruneDaemonWith(t *testing.T, d *pruneDaemon) *pruneDaemon {
	t.Helper()
	dir, err := os.MkdirTemp("", "rexprune")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	d.sock = filepath.Join(dir, "d.sock")
	if d.snapshot == nil {
		d.snapshot = func(int) []protocol.SessionSummary { return pruneSessions }
	}
	ln, err := net.Listen("unix", d.sock)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go d.serve(conn)
		}
	}()
	return d
}

func (d *pruneDaemon) serve(conn net.Conn) {
	defer conn.Close()
	r, w := protocol.NewReader(conn), protocol.NewWriter(conn)
	for {
		env, err := r.Read()
		if err != nil {
			return
		}
		switch env.Type {
		case protocol.IntentHello:
			d.mu.Lock()
			n := d.hellos
			d.hellos++
			d.mu.Unlock()
			snap := protocol.Snapshot{Sessions: d.snapshot(n)}
			if d.acks {
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
			if !d.acks || env.ID == "" {
				continue
			}
			if d.reply != nil {
				d.reply(w, env, del.SessionID)
				continue
			}
			_ = w.WriteEvent(protocol.EventAck, env.ID, protocol.Ack{Intent: protocol.IntentDelete, SessionID: del.SessionID})
		}
	}
}

// withPruneIO installs a Printer whose stdin is stdin and whose terminals
// are interactive or not, with buffers for stdout and stderr. A terminal
// gets the text format (the only one that asks); a pipe gets txt.
func withPruneIO(t *testing.T, stdin string, interactive bool) (*bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	return withPruneIOFormat(t, stdin, interactive, "")
}

// withPruneIOFormat is withPruneIO with the output format chosen (empty:
// text on a terminal, txt on a pipe).
func withPruneIOFormat(t *testing.T, stdin string, interactive bool, format string) (*bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	env := core.Env{
		Getenv: func(k string) string {
			if k == "NO_COLOR" {
				return "1"
			}
			return ""
		},
		InTTY: interactive, ErrTTY: interactive, Width: 80, Scheme: "default", GOOS: "linux",
	}
	p, err := core.NewPrinter(core.FormatFlags{}, env, out, errOut)
	require.NoError(t, err)
	switch {
	case format != "":
		p, err = core.NewPrinter(core.FormatFlags{Output: format}, env, out, errOut)
		require.NoError(t, err)
	case interactive:
		p, err = core.NewPrinter(core.FormatFlags{Output: "text"}, env, out, errOut)
		require.NoError(t, err)
	}
	p.In = strings.NewReader(stdin)
	core.SetOut(p)
	t.Cleanup(func() { core.SetOut(nil) })
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
	out, _ := withPruneIO(t, "", false)
	require.NoError(t, RunPrune([]string{"-socket", d.sock, "-yes"}))
	require.Eventually(t, func() bool { return len(d.Deleted()) == 2 }, 2*time.Second, 10*time.Millisecond)
	require.Equal(t, wantPruned, d.Deleted())
	require.Contains(t, out.String(), "cleared 0, sent 2 unconfirmed", "unacknowledged deletes are not reported as cleared")
}

// The list is taken before the prompt. A marked-done session typed into
// meanwhile loses its mark and works again; it must not be deleted, while
// a finished one still is.
func TestRunPrune_KeepsSessionThatChangedAfterTheList(t *testing.T) {
	idle := protocol.SessionSummary{ID: "55555555-1234-4abc-89ab-cdefabcdef05", ShortID: "5555", Slug: "idle-marked",
		State: protocol.StateNeedsInput, RetireAt: time.Now().Add(time.Hour)}
	finished := pruneSessions[0]
	d := startPruneDaemonWith(t, &pruneDaemon{acks: true, snapshot: func(hellos int) []protocol.SessionSummary {
		if hellos == 0 {
			return []protocol.SessionSummary{idle, finished}
		}
		typedInto := idle
		typedInto.RetireAt, typedInto.State = time.Time{}, protocol.StateWorking
		return []protocol.SessionSummary{typedInto, finished}
	}})
	out, errOut := withPruneIO(t, "y\n", true)
	require.NoError(t, RunPrune([]string{"-socket", d.sock}))
	require.Equal(t, []string{finished.ID}, d.Deleted())
	require.Contains(t, out.String(), "cleared 1, kept 1 that changed")
	require.Contains(t, errOut.String(), "kept 5555 (idle-marked): it changed since the list was taken")
}

func TestRunPrune_AlreadyGoneCountsAsCleared(t *testing.T) {
	d := startPruneDaemonWith(t, &pruneDaemon{acks: true, reply: func(w *protocol.Writer, env protocol.Envelope, _ string) {
		_ = w.WriteEvent(protocol.EventError, env.ID, protocol.ErrorEvent{ID: env.ID, Code: protocol.ErrCodeUnknownSession, Message: "no such session"})
	}})
	out, _ := withPruneIO(t, "", false)
	require.NoError(t, RunPrune([]string{"-socket", d.sock, "-yes"}))
	require.Contains(t, out.String(), "cleared 2")
}

func TestRunPrune_ReportsDaemonRejection(t *testing.T) {
	d := startPruneDaemonWith(t, &pruneDaemon{acks: true, reply: func(w *protocol.Writer, env protocol.Envelope, _ string) {
		_ = w.WriteEvent(protocol.EventError, env.ID, protocol.ErrorEvent{ID: env.ID, Code: protocol.ErrCodeBadIntent, Message: "nope"})
	}})
	_, errOut := withPruneIO(t, "", false)
	err := RunPrune([]string{"-socket", d.sock, "-yes"})
	require.Error(t, err)
	require.Equal(t, core.ExitGeneric, exitCode(t, err))
	require.Contains(t, err.Error(), "2 of 2 failed")
	require.Contains(t, errOut.String(), "nope")
}

func TestRunPrune_RejectsArguments(t *testing.T) {
	err := RunPrune([]string{"some-session"})
	require.Error(t, err)
	require.Equal(t, core.ExitInvalidArgs, exitCode(t, err))
}
