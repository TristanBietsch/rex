package server

import (
	"context"
	"net"
	"os"
	"path/filepath"
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
