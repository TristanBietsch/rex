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
