package pty

import (
	"bytes"
	"context"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/daemon/adapter"
	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// recorder is a writer that records each Write call.
type recorder struct {
	mu     sync.Mutex
	writes []string
	block  chan struct{} // when set, Write waits for it
}

func (r *recorder) Write(p []byte) (int, error) {
	if r.block != nil {
		<-r.block
	}
	r.mu.Lock()
	r.writes = append(r.writes, string(p))
	r.mu.Unlock()
	return len(p), nil
}

func (r *recorder) got() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.writes...)
}

// A paste and its Enter are one unit: input queued meanwhile cannot land
// between them, and a reply is never squeezed in.
func TestPTYWriter_UnitsStayAtomicAndOrdered(t *testing.T) {
	rec := &recorder{}
	done := make(chan struct{})
	defer close(done)
	pw := newPTYWriter(rec, done, nil)
	ctx := context.Background()

	require.True(t, pw.Send(ctx, writeUnit{parts: [][]byte{[]byte("PASTE"), []byte("\r")}, gap: 60 * time.Millisecond}))
	require.True(t, pw.Send(ctx, writeUnit{parts: [][]byte{[]byte("typed")}}))
	time.Sleep(20 * time.Millisecond) // the first unit is mid-sequence now
	require.False(t, pw.TrySendReply([]byte("REPLY")), "dropped while input is mid-sequence or queued")
	require.Eventually(t, func() bool { return len(rec.got()) == 3 }, 3*time.Second, 10*time.Millisecond)
	require.Equal(t, []string{"PASTE", "\r", "typed"}, rec.got())

	require.Eventually(t, func() bool { return pw.TrySendReply([]byte("REPLY")) }, time.Second, 5*time.Millisecond, "an idle writer takes a reply")
	require.Eventually(t, func() bool { return len(rec.got()) == 4 }, time.Second, 10*time.Millisecond)
}

// A child that never reads stdin stalls the writer, not the callers: replies
// are dropped, and Send only blocks once the queue is full.
func TestPTYWriter_StuckChildNeverBlocksReply(t *testing.T) {
	rec := &recorder{block: make(chan struct{})}
	done := make(chan struct{})
	defer close(done)
	defer close(rec.block)
	pw := newPTYWriter(rec, done, nil)
	require.True(t, pw.Send(context.Background(), writeUnit{parts: [][]byte{[]byte("x")}}))
	require.Eventually(t, pw.busy.Load, time.Second, time.Millisecond)

	start := time.Now()
	for range 1000 {
		require.False(t, pw.TrySendReply(bytes.Repeat([]byte("r"), 64)))
	}
	require.Less(t, time.Since(start), time.Second)

	// Send honors its context when the queue is full.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	sent := 0
	for pw.Send(ctx, writeUnit{parts: [][]byte{[]byte("y")}}) {
		sent++
		require.Less(t, sent, 1000)
	}
	require.Equal(t, writerQueue, sent, "the queue is bounded")
}

// The reader keeps draining the child's output even when the child ignores a
// flood of status queries and never reads its stdin.
func TestSupervisor_QueryFloodDoesNotBlockReader(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh and a Unix tty")
	}
	h, err := adapter.NewHeuristic(`x`, "", 0)
	require.NoError(t, err)
	stateDir := t.TempDir()
	store := state.NewStore()
	sess := &state.Session{ID: "flood1", ShortID: "flood1", ToolID: "qwen", Slug: "flood",
		State: protocol.StateQueued, StartedAt: time.Now().UTC()}
	require.NoError(t, store.Add(sess))
	// 3000 queries, none of the replies ever read, then END.
	script := `i=0; while [ $i -lt 3000 ]; do printf '\033]7501;?\007'; i=$((i+1)); done; echo END`
	sup := New(SupervisorConfig{StateDir: stateDir, Store: store, Command: []string{"sh", "-c", script},
		Adapter: adapter.NewOSCStatus(h), IdleTick: 20 * time.Millisecond})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	require.NoError(t, sup.Run(ctx, sess))
	tail, _ := state.TranscriptTail(stateDir, "flood1", 1<<20)
	require.Contains(t, string(tail), "END")
}
