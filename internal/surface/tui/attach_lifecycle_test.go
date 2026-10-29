package tui

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// floodingDaemon accepts one connection on a temp socket and writes n
// SessionOutput events for session "s" to it.
func floodingDaemon(t *testing.T, n int) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "rexq")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "a.sock")
	ln, err := net.Listen("unix", sock)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		t.Cleanup(func() { _ = conn.Close() })
		w := protocol.NewWriter(conn)
		for range n {
			if w.WriteEvent(protocol.EventSessionOutput, "", protocol.SessionOutput{SessionID: "s", Bytes: []byte("x")}) != nil {
				return
			}
		}
	}()
	return sock
}

// After a detach nobody drains outCh. A pump blocked on a full outCh used to
// stay blocked forever (closing the client can't wake a channel send); the
// popup's done channel now releases it, and its listener with it.
func TestAttachPump_ExitsOnCloseWithFullChannel(t *testing.T) {
	sock := floodingDaemon(t, 200)
	c, err := client.Dial(sock)
	require.NoError(t, err)
	st := &AttachState{
		SessionID: "s", Client: c,
		outCh: make(chan AttachOutputMsg, 64),
		errCh: make(chan AttachClosedMsg, 1),
		done:  make(chan struct{}),
	}
	exited := make(chan struct{})
	go func() {
		pumpAttach(c, "s", st.outCh, st.errCh, st.done)
		close(exited)
	}()
	require.Eventually(t, func() bool { return len(st.outCh) == cap(st.outCh) },
		5*time.Second, 5*time.Millisecond, "pump never filled outCh")

	m := closeAttach(Model{Attach: st, Focus: FocusAttach})
	require.Nil(t, m.Attach)
	select {
	case <-exited:
	case <-time.After(2 * time.Second):
		t.Fatal("pump goroutine leaked after closeAttach")
	}
}

func TestListenAttach_EndsWhenPopupCloses(t *testing.T) {
	st := &AttachState{
		SessionID: "s",
		outCh:     make(chan AttachOutputMsg, 1),
		errCh:     make(chan AttachClosedMsg, 1),
		done:      make(chan struct{}),
	}
	got := make(chan any, 1)
	go func() { got <- listenAttach(st)() }()
	close(st.done)
	select {
	case msg := <-got:
		require.Nil(t, msg)
	case <-time.After(2 * time.Second):
		t.Fatal("listener goroutine leaked after the popup closed")
	}
}

// A late message from an earlier popup's listener used to re-arm
// listenAttach on the current popup, leaving two listeners that split and
// reorder its output. Only the current popup's own messages re-arm.
func TestAttachOutput_StaleListenerDoesNotRearm(t *testing.T) {
	m := attachWithScrollback(t, 0) // popup on session "a"
	cur := m.Attach

	_, cmd := m.Update(AttachOutputMsg{SessionID: "b", Bytes: []byte("old"), from: &AttachState{SessionID: "b"}})
	require.Nil(t, cmd, "message from a popup on another session")

	// Detach and reattach the same session: the old listener's message
	// carries the right session id but the wrong popup.
	out, cmd := m.Update(AttachOutputMsg{SessionID: "a", Bytes: []byte("old"), from: &AttachState{SessionID: "a"}})
	require.Nil(t, cmd, "message from an earlier popup on the same session")
	require.NotContains(t, cur.Term.String(), "old")
	out, _ = out.(Model).Update(AttachClosedMsg{SessionID: "a", Err: net.ErrClosed, from: &AttachState{SessionID: "a"}})
	require.False(t, out.(Model).Attach.Ended, "an old popup's close must not end this one")

	_, cmd = m.Update(AttachOutputMsg{SessionID: "a", Bytes: []byte("new"), from: cur})
	require.NotNil(t, cmd, "the current popup's listener re-arms")
}

// openAttach used to dial, subscribe, resize and redraw inside Update, so a
// slow daemon froze input. The socket work now happens in the returned Cmd.
func TestOpenAttach_DialsOffTheEventLoop(t *testing.T) {
	m := boardModel(protocol.SessionSummary{ID: "a", Slug: "a", State: protocol.StateNeedsInput})
	m.Socket = filepath.Join(t.TempDir(), "none.sock")

	m, cmd := openAttach(m, "a")
	require.NotNil(t, cmd)
	require.Empty(t, m.Err, "nothing dialed yet")
	require.Nil(t, m.Attach)

	msg, ok := cmd().(attachDialedMsg)
	require.True(t, ok)
	require.Error(t, msg.Err)
	out, _ := m.Update(msg)
	m = out.(Model)
	require.Contains(t, m.Err, "attach: ")
	require.Equal(t, FocusBoard, m.Focus)
}

func TestAttachOpened_DropsWhenFocusMovedOn(t *testing.T) {
	d := startFakeBoardDaemon(t)
	c, err := client.Dial(d.Socket)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	m := boardModel(protocol.SessionSummary{ID: "a", Slug: "a", State: protocol.StateNeedsInput})
	m.Focus = FocusHelp // opened while the dial was in flight
	m, _ = attachOpened(m, attachDialedMsg{SessionID: "a", C: c, Cols: 80, Rows: 24})
	require.Nil(t, m.Attach)
	require.Equal(t, FocusHelp, m.Focus)
	require.Error(t, c.SendInput("a", []byte("x")), "the unused connection is closed")
}
