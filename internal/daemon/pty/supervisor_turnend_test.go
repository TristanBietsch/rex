package pty

import (
	"bytes"
	"context"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// textAdapter says needs_input once the agent has printed its final text.
type textAdapter struct{ stubAdapter }

func (a *textAdapter) Detect(window []byte, _ time.Duration) protocol.State {
	if bytes.Contains(window, []byte("FINAL-TEXT")) {
		return protocol.StateNeedsInput
	}
	return protocol.StateWorking
}

// At the moment the supervisor publishes a turn end (needs_input), the
// session's last_line and screen already show the agent's final output: a
// subscriber (the loop runner) reading them then must not see stale text.
func TestSupervisor_ScreenIsFreshWhenNeedsInputIsPublished(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the child is a Unix shell")
	}
	store := state.NewStore()
	sess := &state.Session{ID: "turn1", ShortID: "turn1", ToolID: "claude", Slug: "turn",
		State: protocol.StateQueued, StartedAt: time.Now().UTC()}
	require.NoError(t, store.Add(sess))
	var (
		mu       sync.Mutex
		atEvent  []string
		lastLine []string
	)
	store.Subscribe(func(e state.Event) {
		if e.NewState == nil || *e.NewState != protocol.StateNeedsInput {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		atEvent = append(atEvent, store.Screen("turn1"))
		lastLine = append(lastLine, sess.Summary().LastLine)
	})
	sup := New(SupervisorConfig{
		StateDir: t.TempDir(), Store: store,
		Command:   []string{"sh", "-c", "printf 'FINAL-TEXT\\n'; sleep 0.6"},
		ReadyHold: -1, // not about the hold
		Adapter:   &textAdapter{},
		InputCh:   make(chan []byte, 1),
		IdleTick:  20 * time.Millisecond,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, sup.Run(ctx, sess))

	mu.Lock()
	defer mu.Unlock()
	require.NotEmpty(t, atEvent, "the turn end was published")
	require.True(t, strings.Contains(atEvent[0], "FINAL-TEXT"), "screen at the needs_input event: %q", atEvent[0])
	require.Contains(t, lastLine[0], "FINAL-TEXT")
}
