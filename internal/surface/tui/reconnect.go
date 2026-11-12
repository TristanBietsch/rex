package tui

import (
	"errors"
	"io"
	"log/slog"
	"math/rand/v2"
	"net"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// Daemon reconnect (backlog P0-06).
//
// When the board's daemon connection dies (rex daemon restart, crash,
// upgrade) the TUI keeps its board on screen, shows reconnectStatus and
// redials with capped exponential backoff. A redial replays the boot
// handshake (Dial, Hello, Subscribe) and swaps in the daemon's session list.
// It only ever dials: starting a daemon is the boot splash's job, and doing
// it from here would fight `rex daemon stop`.
//
// Invariant: at most one of {listenDaemon chain, reconnect attempt} is in
// flight. A dead connection ends the listen chain (it is not re-armed) and
// starts the reconnect loop; a successful redial ends the loop and arms a
// fresh listen chain on the new client.

const (
	reconnectBaseDelay    = 250 * time.Millisecond
	reconnectMaxDelay     = 5 * time.Second
	reconnectJitter       = 0.2 // a delay is shortened by up to 20%
	reconnectHelloTimeout = 3 * time.Second
	reconnectStatus       = "daemon disconnected — reconnecting…"
)

// reconnectState is set on Model while the daemon connection is down.
type reconnectState struct {
	attempt int       // redial attempts that failed so far
	since   time.Time // when the connection was lost
	lastErr error     // why the connection or the last redial failed
}

// daemonDialer connects to the daemon at socket and performs the board
// handshake, returning a subscribed client and the session snapshot.
type daemonDialer func(socket string) (*client.Client, *protocol.Snapshot, error)

// reconnectTickMsg fires when the backoff for redial attempt n has elapsed.
type reconnectTickMsg struct{ attempt int }

// reconnectResultMsg carries the outcome of redial attempt n.
type reconnectResultMsg struct {
	attempt int
	client  *client.Client
	snap    *protocol.Snapshot
	err     error
}

// isConnClosed reports whether a listenDaemon error means the connection is
// gone for good, as opposed to one bad line on a live stream. EOF and a
// closed or reset socket qualify; a read timeout and a decode error don't.
func isConnClosed(err error) bool {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed) {
		return true
	}
	var opErr *net.OpError
	return errors.As(err, &opErr) && !opErr.Timeout()
}

// reconnectDelay is the wait before redial attempt n (0-based): 250ms
// doubling per attempt, capped at 5s. jitter in [0, 1) shortens it by up to
// reconnectJitter so TUIs that lost the same daemon don't redial in
// lockstep; the cap is never exceeded.
func reconnectDelay(attempt int, jitter float64) time.Duration {
	d := reconnectBaseDelay
	for i := 0; i < attempt && d < reconnectMaxDelay; i++ {
		d *= 2
	}
	d = min(d, reconnectMaxDelay)
	return d - time.Duration(jitter*reconnectJitter*float64(d))
}

// scheduleReconnect arms the backoff timer for redial attempt n.
func scheduleReconnect(attempt int) tea.Cmd {
	d := reconnectDelay(attempt, rand.Float64()) //nolint:gosec // G404: backoff jitter, not a secret.
	return tea.Tick(d, func(time.Time) tea.Msg { return reconnectTickMsg{attempt: attempt} })
}

// startReconnect handles a dead daemon connection: it closes the client
// (a later intent then fails fast instead of writing into a dead socket),
// shows the disconnected status, and schedules the first redial. It does
// not re-arm listenDaemon: Read on a dead connection returns EOF at once,
// so re-arming spun the TUI at 100% CPU.
func startReconnect(m Model, cause error) (Model, tea.Cmd) {
	if m.Client != nil {
		_ = m.Client.Close()
	}
	m.reconnect = &reconnectState{since: time.Now(), lastErr: cause}
	slog.Warn("tui: daemon connection lost; reconnecting", "err", cause)
	return m, scheduleReconnect(0)
}

// handleReconnectTick starts the redial the timer was armed for. A tick for
// any other attempt (or after reconnecting) is stale and dropped.
func handleReconnectTick(m Model, msg reconnectTickMsg) (Model, tea.Cmd) {
	if m.reconnect == nil || msg.attempt != m.reconnect.attempt {
		return m, nil
	}
	dial := m.dialDaemon
	if dial == nil {
		dial = handshakeDaemon
	}
	socket := m.Socket
	return m, func() tea.Msg {
		c, snap, err := dial(socket)
		return reconnectResultMsg{attempt: msg.attempt, client: c, snap: snap, err: err}
	}
}

// handleReconnectResult backs off and retries on failure, or adopts the new
// connection and resumes listening on success.
func handleReconnectResult(m Model, msg reconnectResultMsg) (Model, tea.Cmd) {
	if m.reconnect == nil || msg.attempt != m.reconnect.attempt {
		if msg.client != nil {
			_ = msg.client.Close() // stale: never leak a connection
		}
		return m, nil
	}
	if msg.err != nil {
		m.reconnect.attempt++
		m.reconnect.lastErr = msg.err
		slog.Debug("tui: reconnect failed", "attempt", msg.attempt, "err", msg.err)
		return m, scheduleReconnect(m.reconnect.attempt)
	}
	slog.Info("tui: reconnected to daemon",
		"attempts", msg.attempt+1, "down_ms", time.Since(m.reconnect.since).Milliseconds())
	m = adoptConnection(m, msg.client, msg.snap)
	return m, listenDaemon(m.Client)
}

// adoptConnection swaps in the new client and the daemon's current session
// list. The selection survives by session ID when that session still
// exists, else it moves to the first row. Changes made while disconnected
// just re-render: no sounds or animations replay for the gap. Errors from
// the disconnected period are stale, so the status line is cleared.
func adoptConnection(m Model, c *client.Client, snap *protocol.Snapshot) Model {
	m.Client = c
	m.reconnect = nil
	m = adoptSessions(m, snap.Sessions)
	m.Overview = snap.Overview
	m = adoptWorkspaceTagging(m, snap)
	m = adoptLoops(m, snap)
	m = reselectIfGone(adoptSnapshotFilter(m, snap.Filter))
	m.Err = ""
	m.Notice = "reconnected to daemon"
	return ensureVisible(m)
}

// handshakeDaemon dials the daemon and replays the boot handshake: Hello
// (which answers with the snapshot), then a board-wide Subscribe. A missing
// socket is an error to retry, never a reason to spawn a daemon. Hello runs
// under a read deadline so a wedged daemon can't hang the redial forever.
func handshakeDaemon(socket string) (*client.Client, *protocol.Snapshot, error) {
	c, err := client.Dial(socket)
	if err != nil {
		return nil, nil, err
	}
	snap, err := helloWithDeadline(c)
	if err == nil {
		err = c.Subscribe("")
	}
	if err != nil {
		_ = c.Close()
		return nil, nil, err
	}
	return c, snap, nil
}

func helloWithDeadline(c *client.Client) (*protocol.Snapshot, error) {
	if err := c.SetReadDeadline(time.Now().Add(reconnectHelloTimeout)); err != nil {
		return nil, err
	}
	snap, err := c.Hello("rex-tui")
	if err != nil {
		return nil, err
	}
	// The listen chain blocks in Read for as long as the board is idle.
	return snap, c.SetReadDeadline(time.Time{})
}
