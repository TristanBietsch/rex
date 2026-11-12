package pty

import (
	"context"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func TestIsInterruptKey(t *testing.T) {
	for _, k := range []string{"\x1b", "\x03", "\x1b[27u", "\x1b[27;1u", "\x1b[27;1:1u", "\x1b[99;5u", "\x1b[27;5;99~"} {
		require.True(t, isInterruptKey([]byte(k)), "%q", k)
	}
	for _, k := range []string{"", "a", "\x1b\x1b", "\x1b[A", "\x1b[27;1:3u", "\x1b[200~hi\x1b[201~", "q\x1b", "\r"} {
		require.False(t, isInterruptKey([]byte(k)), "%q", k)
	}
}

// interruptAdapter counts UserInterrupt calls.
type interruptAdapter struct {
	stubAdapter
	n atomic.Int32
}

func (a *interruptAdapter) UserInterrupt() { a.n.Add(1) }

// The supervisor tells the adapter about an interrupt key from a client,
// and only about that.
func TestSupervisor_InterruptKeyTellsAdapter(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the child reads its input from a Unix tty")
	}
	store := state.NewStore()
	sess := &state.Session{ID: "esc1", ShortID: "esc1", ToolID: "claude", Slug: "esc",
		State: protocol.StateQueued, StartedAt: time.Now().UTC()}
	require.NoError(t, store.Add(sess))
	ad := &interruptAdapter{}
	input := make(chan []byte, 4)
	input <- []byte("a")
	input <- []byte("\x1b[A")
	input <- []byte{0x1b}
	input <- []byte("\n")
	sup := New(SupervisorConfig{
		StateDir: t.TempDir(), Store: store,
		Command:  []string{"sh", "-c", "read x"},
		Adapter:  ad,
		InputCh:  input,
		IdleTick: 20 * time.Millisecond,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, sup.Run(ctx, sess))
	require.Equal(t, int32(1), ad.n.Load())
}
