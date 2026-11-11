package pty

import (
	"context"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// observerAdapter records every chunk it is shown.
type observerAdapter struct {
	stubAdapter
	mu  sync.Mutex
	buf []byte
}

func (o *observerAdapter) Observe(chunk []byte) {
	o.mu.Lock()
	o.buf = append(o.buf, chunk...) // chunk is only valid during the call
	o.mu.Unlock()
}

func (o *observerAdapter) seen() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return string(o.buf)
}

// An adapter that implements OutputObserver is shown every PTY chunk, in
// order, including escape-only ones the idle timer ignores.
func TestSupervisor_FeedsOutputObserver(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh and a Unix tty")
	}
	store := state.NewStore()
	sess := &state.Session{ID: "obs1", ShortID: "obs1", ToolID: "antigravity", Slug: "obs",
		State: protocol.StateQueued, StartedAt: time.Now().UTC()}
	require.NoError(t, store.Add(sess))
	ad := &observerAdapter{}
	sup := New(SupervisorConfig{
		StateDir: t.TempDir(), Store: store,
		Command:  []string{"sh", "-c", `printf AAA; sleep 0.1; printf '\033]7501;state=idle\007'; sleep 0.1; printf CCC`},
		Adapter:  ad,
		IdleTick: 20 * time.Millisecond,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, sup.Run(ctx, sess))
	require.Equal(t, "AAA\x1b]7501;state=idle\x07CCC", ad.seen())
}
