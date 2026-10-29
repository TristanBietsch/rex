package tui

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// fakeBoardDaemon answers the board handshake (Hello → Snapshot, then
// Subscribe) on a temp socket and hands each handshaken connection to conns.
type fakeBoardDaemon struct {
	Socket string
	conns  chan net.Conn

	mu   sync.Mutex
	snap protocol.Snapshot
}

func startFakeBoardDaemon(t *testing.T, sessions ...protocol.SessionSummary) *fakeBoardDaemon {
	t.Helper()
	// MkdirTemp, not t.TempDir: unix socket paths must stay under 104 bytes.
	dir, err := os.MkdirTemp("", "rexq")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	d := &fakeBoardDaemon{
		Socket: filepath.Join(dir, "d.sock"),
		conns:  make(chan net.Conn, 4),
		snap:   protocol.Snapshot{Sessions: sessions},
	}
	ln, err := net.Listen("unix", d.Socket)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			t.Cleanup(func() { _ = conn.Close() })
			go d.handshake(conn)
		}
	}()
	return d
}

func (d *fakeBoardDaemon) handshake(conn net.Conn) {
	r := protocol.NewReader(conn)
	if _, err := r.Read(); err != nil { // Hello
		return
	}
	d.mu.Lock()
	snap := d.snap
	d.mu.Unlock()
	if err := protocol.NewWriter(conn).WriteEvent(protocol.EventSnapshot, "h", snap); err != nil {
		return
	}
	if _, err := r.Read(); err != nil { // Subscribe
		return
	}
	d.conns <- conn
}

func (d *fakeBoardDaemon) setSessions(sessions ...protocol.SessionSummary) {
	d.mu.Lock()
	d.snap = protocol.Snapshot{Sessions: sessions}
	d.mu.Unlock()
}

func (d *fakeBoardDaemon) nextConn(t *testing.T) net.Conn {
	t.Helper()
	select {
	case c := <-d.conns:
		return c
	case <-time.After(5 * time.Second):
		t.Fatal("no handshaken connection")
		return nil
	}
}

// awaitMsg executes cmd and returns its message, failing after a timeout. A
// batch runs its commands concurrently and yields the first non-nil message.
func awaitMsg(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	require.NotNil(t, cmd)
	ch := make(chan tea.Msg, 8)
	var run func(tea.Cmd)
	run = func(c tea.Cmd) {
		msg := c()
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, sub := range batch {
				if sub != nil {
					go run(sub)
				}
			}
			return
		}
		if msg != nil {
			ch <- msg
		}
	}
	go run(cmd)
	select {
	case msg := <-ch:
		return msg
	case <-time.After(5 * time.Second):
		t.Fatal("command produced no message")
		return nil
	}
}

// When the daemon connection dies the board must stop reading from it (the
// old code re-armed the listener, which failed instantly forever and spun at
// 100% CPU), say so, and redial until the daemon is back.
func TestDaemonConnLost_RedialsInsteadOfSpinning(t *testing.T) {
	a := protocol.SessionSummary{ID: "a", Slug: "a", State: protocol.StateDone}
	d := startFakeBoardDaemon(t, a)
	c, err := client.Dial(d.Socket)
	require.NoError(t, err)
	_, err = c.Hello("test")
	require.NoError(t, err)
	require.NoError(t, c.Subscribe(""))
	conn1 := d.nextConn(t)

	m := boardModel(a)
	m.Client, m.Socket = c, d.Socket

	require.NoError(t, conn1.Close())
	errMsg, ok := awaitMsg(t, listenDaemon(m.Client)).(DaemonErrMsg)
	require.True(t, ok)
	require.True(t, daemonConnDead(errMsg.Err), "EOF must count as dead: %v", errMsg.Err)

	out, cmd := m.Update(errMsg)
	m = out.(Model)
	require.Equal(t, daemonDownStatus, m.Err)
	// Re-arming the listener would answer with another DaemonErrMsg at once.
	msg := awaitMsg(t, cmd)
	require.Equal(t, daemonRedialMsg{Attempt: 0}, msg)

	b := protocol.SessionSummary{ID: "b", Slug: "b", State: protocol.StateDone}
	d.setSessions(a, b)
	out, cmd = m.Update(msg)
	m = out.(Model)
	res, ok := awaitMsg(t, cmd).(daemonRedialResultMsg)
	require.True(t, ok)
	require.NoError(t, res.Err)

	out, cmd = m.Update(res)
	m = out.(Model)
	require.NotSame(t, c, m.Client)
	require.Len(t, m.Sessions, 2, "snapshot from the new connection replaces the board")
	require.Empty(t, m.Err)
	require.Equal(t, "reconnected to daemon", m.Notice)

	// Listening again, on the new connection.
	conn2 := d.nextConn(t)
	require.NoError(t, protocol.NewWriter(conn2).WriteEvent(protocol.EventSessionAdded, "",
		protocol.SessionSummary{ID: "c", Slug: "c"}))
	ev, ok := awaitMsg(t, cmd).(DaemonEventMsg)
	require.True(t, ok)
	require.Equal(t, protocol.EventSessionAdded, ev.Env.Type)
}

func TestDaemonRedial_FailureBacksOffAndKeepsStatus(t *testing.T) {
	m := boardModel()
	m.Socket = "/nonexistent/rex.sock"
	m.Err = "" // the user may have cleared it with ':'
	out, cmd := m.Update(daemonRedialResultMsg{Err: errors.New("dial: refused"), Attempt: 2})
	require.Equal(t, daemonDownStatus, out.(Model).Err)
	require.NotNil(t, cmd)

	require.Equal(t, redialBase, redialDelay(0))
	require.Equal(t, 2*redialBase, redialDelay(1))
	require.Equal(t, redialMax, redialDelay(10))
	require.Equal(t, redialMax, redialDelay(1000), "no shift overflow")
}

func TestDaemonConnDead(t *testing.T) {
	decodeErr := json.Unmarshal([]byte("{"), new(map[string]any))
	require.Error(t, decodeErr)
	tests := []struct {
		err  error
		dead bool
	}{
		{io.EOF, true},
		{fmt.Errorf("read line: %w", net.ErrClosed), true},
		{fmt.Errorf("read line: %w", &net.OpError{Op: "read", Err: errors.New("connection reset by peer")}), true},
		{fmt.Errorf("decode envelope: %w", decodeErr), false},
		{errors.New("unsupported protocol version 9 (want 1)"), false},
	}
	for _, tt := range tests {
		require.Equal(t, tt.dead, daemonConnDead(tt.err), "%v", tt.err)
	}
}
