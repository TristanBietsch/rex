package pty

import (
	"bytes"
	"context"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hinshun/vt10x"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// TestSupervisor_AttachHookHoldsOutput: while the attach hook runs, no
// output reaches subscribers or the screen; it arrives once the hook
// returns. The hook's repaint shows what the child drew.
func TestSupervisor_AttachHookHoldsOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the child reads its cue from a Unix tty")
	}
	store := state.NewStore()
	sess := &state.Session{ID: "hold1", ShortID: "hold1", ToolID: "echo", Slug: "test",
		State: protocol.StateQueued, StartedAt: time.Now().UTC()}
	require.NoError(t, store.Add(sess))

	var mu sync.Mutex
	var sink bytes.Buffer
	sunk := func() string {
		mu.Lock()
		defer mu.Unlock()
		return sink.String()
	}
	hookCh := make(chan func(fn func(repaint func() []byte)), 1)
	unregistered := make(chan struct{})
	in := make(chan []byte, 1)
	sup := New(SupervisorConfig{
		StateDir: t.TempDir(), Store: store,
		Command: []string{"bash", "-c", "stty -echo; printf 'FIRST'; read -r _; printf 'SECOND'; read -r _"},
		InputCh: in,
		OutputSink: func(b []byte) {
			mu.Lock()
			sink.Write(b)
			mu.Unlock()
		},
		RegisterAttach:   func(h func(fn func(repaint func() []byte))) { hookCh <- h },
		UnregisterAttach: func() { close(unregistered) },
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- sup.Run(ctx, sess) }()
	hook := <-hookCh

	screenOf := func() string {
		var b []byte
		hook(func(repaint func() []byte) { b = repaint() })
		term := vt10x.New(vt10x.WithSize(120, 32))
		_, _ = term.Write(b)
		return term.String()
	}
	require.Eventually(t, func() bool { return strings.Contains(screenOf(), "FIRST") }, 5*time.Second, 10*time.Millisecond)

	var during, held string
	hook(func(repaint func() []byte) {
		in <- []byte("go\n")
		time.Sleep(300 * time.Millisecond) // the child answers meanwhile
		during = sunk()
		term := vt10x.New(vt10x.WithSize(120, 32))
		_, _ = term.Write(repaint())
		held = term.String()
	})
	require.NotContains(t, during, "SECOND", "output reached a subscriber while held")
	require.NotContains(t, held, "SECOND", "output reached the screen while held")
	require.Eventually(t, func() bool { return strings.Contains(sunk(), "SECOND") }, 5*time.Second, 10*time.Millisecond)
	require.Contains(t, screenOf(), "FIRSTSECOND")

	in <- []byte("bye\n")
	require.NoError(t, <-done)
	<-unregistered
}
