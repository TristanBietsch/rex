package server

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// Reply must type the text and then press Enter (\r) as a separate write:
// Claude Code's TUI inserts a newline on \n and treats text+\r in one burst
// as a paste, so neither submits.
func TestIntentReply_TextThenSeparateCarriageReturn(t *testing.T) {
	dir, err := os.MkdirTemp("", "rex-rp")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "rex.sock")
	reg, err := registry.Load("")
	require.NoError(t, err)
	srv, err := New(Config{Socket: sock, StateDir: dir, Registry: reg, Store: state.NewStore()})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = srv.Serve(ctx) }()
	require.Eventually(t, func() bool { _, err := os.Stat(sock); return err == nil }, 2*time.Second, 10*time.Millisecond)

	ch := make(chan []byte, 4)
	srv.RegisterInputChannel("sess-1", ch)

	conn, err := net.Dial("unix", sock)
	require.NoError(t, err)
	defer conn.Close()
	w, _ := helloOnConn(t, conn)
	require.NoError(t, w.WriteIntent(protocol.IntentReply, "r", protocol.Reply{SessionID: "sess-1", Text: "blue"}))

	recv := func() []byte {
		select {
		case b := <-ch:
			return b
		case <-time.After(2 * time.Second):
			t.Fatal("no input delivered")
			return nil
		}
	}
	require.Equal(t, []byte("blue"), recv())
	require.Equal(t, []byte{'\r'}, recv())
}

// setReplyDelay stretches (or shrinks) the Reply text-to-Enter delay.
func setReplyDelay(t *testing.T, d time.Duration) {
	t.Helper()
	old := replySubmitDelay
	replySubmitDelay = d
	t.Cleanup(func() { replySubmitDelay = old })
}

// The Enter delay must not stall the connection: intents sent right after
// a Reply are handled before its Enter goes out.
func TestIntentReply_DoesNotBlockReadLoop(t *testing.T) {
	setReplyDelay(t, 2*time.Second)
	e := startRetireServer(t)
	ch := make(chan []byte, 4)
	e.srv.RegisterInputChannel("sess-1", ch)
	conn, w, r := e.dial(t)

	start := time.Now()
	require.NoError(t, w.WriteIntent(protocol.IntentReply, "r", protocol.Reply{SessionID: "sess-1", Text: "blue"}))
	require.NoError(t, w.WriteIntent(protocol.IntentSubscribe, "next", protocol.Subscribe{SessionID: "nope"}))
	waitEvent(t, conn, r, errorFor("next"))
	require.Less(t, time.Since(start), time.Second, "next intent waited for the Reply's Enter")
	require.Equal(t, []byte("blue"), <-ch)
	require.Empty(t, ch, "Enter must still be pending")
	select {
	case b := <-ch:
		require.Equal(t, []byte{'\r'}, b)
	case <-time.After(4 * time.Second):
		t.Fatal("Enter never sent")
	}
}

// Input sent while a Reply's Enter is pending must land after the Enter,
// even from another connection.
func TestIntentReply_LaterInputWaitsForEnter(t *testing.T) {
	setReplyDelay(t, 100*time.Millisecond)
	e := startRetireServer(t)
	ch := make(chan []byte, 8)
	e.srv.RegisterInputChannel("sess-1", ch)
	_, w1, _ := e.dial(t)
	_, w2, _ := e.dial(t)

	recv := func() []byte {
		select {
		case b := <-ch:
			return b
		case <-time.After(2 * time.Second):
			t.Fatal("no input delivered")
			return nil
		}
	}
	require.NoError(t, w1.WriteIntent(protocol.IntentReply, "", protocol.Reply{SessionID: "sess-1", Text: "blue"}))
	require.Equal(t, []byte("blue"), recv())
	require.NoError(t, w2.WriteIntent(protocol.IntentSendInput, "", protocol.SendInput{SessionID: "sess-1", Bytes: []byte("x")}))
	require.NoError(t, w1.WriteIntent(protocol.IntentReply, "", protocol.Reply{SessionID: "sess-1", Text: "y"}))
	require.Equal(t, []byte{'\r'}, recv(), "nothing may overtake the pending Enter")
	next := [][]byte{recv(), recv(), recv()}
	// x and the second Reply race between connections, but the second
	// Reply's Enter must directly follow its text.
	require.Contains(t, next, []byte("x"))
	iy := slices.IndexFunc(next, func(b []byte) bool { return string(b) == "y" })
	require.GreaterOrEqual(t, iy, 0)
	require.Less(t, iy, 2)
	require.Equal(t, []byte{'\r'}, next[iy+1])
}

// Reply text is typed, not interpreted: no escapes, no early submit.
func TestSanitizeReplyText(t *testing.T) {
	require.Equal(t, "a[31mb\nc\td", sanitizeReplyText("a\x1b[31mb\r\nc\x00\td"))
	require.Equal(t, "ok é", sanitizeReplyText("ok\x7f \u009bé\xff"))
}
