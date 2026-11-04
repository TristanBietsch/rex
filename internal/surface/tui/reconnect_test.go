package tui

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/testutil"
	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// runUntil runs cmd, and every command inside a batch, and collects the
// messages they produce until one of type T arrives or timeout passes.
// Commands still blocked afterwards (a listen on a live socket) end when
// the test's cleanups close their connections.
func runUntil[T tea.Msg](cmd tea.Cmd, timeout time.Duration) (got T, ok bool, seen []tea.Msg) {
	out := make(chan tea.Msg, 64)
	var start func(tea.Cmd)
	start = func(c tea.Cmd) {
		if c == nil {
			return
		}
		go func() {
			msg := c()
			if batch, isBatch := msg.(tea.BatchMsg); isBatch {
				for _, sub := range batch {
					start(sub)
				}
				return
			}
			out <- msg
		}()
	}
	start(cmd)
	deadline := time.After(timeout)
	for {
		select {
		case msg := <-out:
			seen = append(seen, msg)
			if t, match := msg.(T); match {
				return t, true, seen
			}
		case <-deadline:
			return got, false, seen
		}
	}
}

func update(m Model, msg tea.Msg) (Model, tea.Cmd) {
	next, cmd := m.Update(msg)
	return next.(Model), cmd
}

// dialFake connects a real client to a fake daemon and returns both.
func dialFake(t *testing.T) (*fakeDaemon, *client.Client) {
	t.Helper()
	fd := newFakeDaemon(t)
	c, err := client.Dial(fd.socket())
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	fd.accept(t)
	return fd, c
}

func TestReconnectDelay_Schedule(t *testing.T) {
	want := []time.Duration{
		250 * time.Millisecond, 500 * time.Millisecond, time.Second,
		2 * time.Second, 4 * time.Second, 5 * time.Second, 5 * time.Second,
	}
	for n, w := range want {
		require.Equal(t, w, reconnectDelay(n, 0), "attempt %d", n)
		j := reconnectDelay(n, 0.999)
		require.LessOrEqual(t, j, w, "jitter never exceeds the schedule")
		require.GreaterOrEqual(t, j, w*8/10, "jitter shortens by at most 20%%")
	}
	require.Equal(t, reconnectMaxDelay, reconnectDelay(1_000_000, 0), "no overflow on long outages")
}

func TestIsConnClosed(t *testing.T) {
	reset := &net.OpError{Op: "read", Net: "unix", Err: errors.New("connection reset by peer")}
	timeout := &net.OpError{Op: "read", Net: "unix", Err: os.ErrDeadlineExceeded}
	for err, want := range map[error]bool{
		io.EOF: true,
		fmt.Errorf("read line: %w", io.ErrUnexpectedEOF):       true,
		fmt.Errorf("read line: %w", net.ErrClosed):             true,
		fmt.Errorf("read line: %w", reset):                     true,
		fmt.Errorf("read line: %w", timeout):                   false,
		fmt.Errorf("decode envelope: %w", &json.SyntaxError{}): false,
		errors.New("unsupported protocol version 9 (want 1)"):  false,
	} {
		require.Equal(t, want, isConnClosed(err), "%v", err)
	}
}

// A dead connection must not re-arm listenDaemon: Read returns EOF at once,
// so the old handler spun the TUI at 100% CPU and never reconnected
// (backlog P0-06). It closes the client, shows the disconnected status and
// schedules the first redial instead.
func TestDaemonErr_EOFStopsListeningAndSchedulesRedial(t *testing.T) {
	_, c := dialFake(t)
	m := boardModel(protocol.SessionSummary{ID: "a", ShortID: "aaaa", Slug: "a", State: protocol.StateNeedsInput})
	m.Client = c

	m, cmd := update(m, DaemonErrMsg{Err: io.EOF})
	require.NotNil(t, m.reconnect, "reconnect loop started")
	require.Contains(t, ansi.Strip(m.View()), reconnectStatus)
	_, err := c.NextEvent()
	require.ErrorIs(t, err, net.ErrClosed, "old client closed")

	tick, ok, seen := runUntil[reconnectTickMsg](cmd, 2*time.Second)
	require.True(t, ok, "first redial scheduled; got %v", seen)
	require.Equal(t, 0, tick.attempt)
	for _, msg := range seen {
		_, relisten := msg.(DaemonErrMsg)
		require.False(t, relisten, "listenDaemon re-armed on the dead connection")
	}

	// A stray error while already reconnecting doesn't start a second loop.
	m2, cmd2 := m.dispatch(DaemonErrMsg{Err: io.EOF})
	require.Nil(t, cmd2)
	require.Equal(t, m.reconnect, m2.(Model).reconnect)
}

// One undecodable line on a live stream keeps the old behavior: show it and
// keep listening on the same connection.
func TestDaemonErr_TransientErrorKeepsListening(t *testing.T) {
	fd, c := dialFake(t)
	m := boardModel()
	m.Client = c

	m, cmd := update(m, DaemonErrMsg{Err: fmt.Errorf("decode envelope: %w", errors.New("bad json"))})
	require.Nil(t, m.reconnect)
	require.Contains(t, m.Err, "decode envelope")

	fd.setState(t, "x", protocol.StateWorking)
	_, ok, seen := runUntil[DaemonEventMsg](cmd, 2*time.Second)
	require.True(t, ok, "still listening on the same client; got %v", seen)
}

// A failed redial backs off and retries; a successful one replaces the
// sessions, keeps the selection by ID, clears stale errors and resumes
// listening on the new client.
func TestReconnect_RetriesThenAdoptsSnapshot(t *testing.T) {
	fd := newFakeDaemon(t)
	m := boardModel(
		protocol.SessionSummary{ID: "a", ShortID: "aaaa", Slug: "a", State: protocol.StateNeedsInput},
		protocol.SessionSummary{ID: "b", ShortID: "bbbb", Slug: "b", State: protocol.StateWorking},
		protocol.SessionSummary{ID: "c", ShortID: "cccc", Slug: "c", State: protocol.StateDone},
	)
	m.SelectedID = "b"
	m.Socket = fd.socket()
	m.reconnect = &reconnectState{since: time.Now()}
	m.Err = "spawn: use of closed network connection"

	calls := 0
	m.dialDaemon = func(socket string) (*client.Client, *protocol.Snapshot, error) {
		calls++
		if socket != fd.socket() {
			return nil, nil, fmt.Errorf("dialed %q", socket)
		}
		if calls == 1 {
			return nil, nil, errors.New("connection refused")
		}
		c, err := client.Dial(socket)
		if err != nil {
			return nil, nil, err
		}
		return c, &protocol.Snapshot{Sessions: []protocol.SessionSummary{
			{ID: "b", ShortID: "bbbb", Slug: "b", State: protocol.StateNeedsInput},
			{ID: "d", ShortID: "dddd", Slug: "d", State: protocol.StateQueued},
		}}, nil
	}

	// Attempt 0 fails: back off and arm attempt 1.
	m, cmd := update(m, reconnectTickMsg{attempt: 0})
	res, ok, _ := runUntil[reconnectResultMsg](cmd, 2*time.Second)
	require.True(t, ok)
	m, cmd = update(m, res)
	require.Equal(t, 1, m.reconnect.attempt)
	require.NotNil(t, cmd, "next redial scheduled")
	require.Contains(t, ansi.Strip(m.View()), reconnectStatus, "status outranks the stale error")

	// A tick for an attempt already done is stale.
	_, stale := m.dispatch(reconnectTickMsg{attempt: 0})
	require.Nil(t, stale)

	// Attempt 1 succeeds.
	m, cmd = update(m, reconnectTickMsg{attempt: 1})
	res, ok, _ = runUntil[reconnectResultMsg](cmd, 2*time.Second)
	require.True(t, ok)
	require.NoError(t, res.err)
	m, listen := update(m, res)
	fd.accept(t)
	t.Cleanup(func() { _ = m.Client.Close() })

	require.Nil(t, m.reconnect)
	require.Same(t, res.client, m.Client)
	require.Equal(t, []string{"b", "d"}, sessionIDs(m.Sessions))
	require.Equal(t, "b", m.SelectedID, "selection kept by ID")
	require.Empty(t, m.Err)
	require.Equal(t, "reconnected to daemon", m.Notice)
	require.Equal(t, 2, calls)

	fd.setState(t, "d", protocol.StateWorking)
	_, ok, seen := runUntil[DaemonEventMsg](listen, 2*time.Second)
	require.True(t, ok, "listening on the new client; got %v", seen)
}

// A selection whose session is gone after the redial moves to the first row.
func TestAdoptConnection_SelectionFallsBackToFirstRow(t *testing.T) {
	m := boardModel(protocol.SessionSummary{ID: "gone", State: protocol.StateWorking})
	m = adoptConnection(m, nil, &protocol.Snapshot{Sessions: []protocol.SessionSummary{
		{ID: "w", State: protocol.StateWorking},
		{ID: "n", State: protocol.StateNeedsInput},
	}})
	require.Equal(t, "n", m.SelectedID, "first row in board order")

	m = adoptConnection(m, nil, &protocol.Snapshot{})
	require.Empty(t, m.SelectedID, "empty board")
}

// A result that arrives after the loop ended must not leak its connection.
func TestReconnect_StaleResultClosesClient(t *testing.T) {
	_, c := dialFake(t)
	m := boardModel()
	m, cmd := update(m, reconnectResultMsg{attempt: 0, client: c, snap: &protocol.Snapshot{}})
	require.Nil(t, cmd)
	require.Nil(t, m.Client)
	_, err := c.NextEvent()
	require.ErrorIs(t, err, net.ErrClosed)
}

// The redial only dials: with no daemon it fails and starts nothing.
func TestHandshakeDaemon_NoDaemonNeverSpawns(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "rex.sock")
	_, _, err := handshakeDaemon(sock)
	require.Error(t, err)
	_, statErr := os.Stat(sock)
	require.ErrorIs(t, statErr, os.ErrNotExist, "no daemon was started on the socket")
}

// Against a real daemon the redial completes Dial, Hello and Subscribe.
func TestHandshakeDaemon_RealDaemon(t *testing.T) {
	d := testutil.StartDaemon(t)
	t.Cleanup(d.Close)
	c, snap, err := handshakeDaemon(d.Socket)
	require.NoError(t, err)
	require.NotNil(t, snap)
	require.NoError(t, c.Close())
}
