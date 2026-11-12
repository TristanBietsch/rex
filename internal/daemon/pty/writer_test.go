package pty

import (
	"bytes"
	"context"
	"runtime"
	"sync"
	"sync/atomic"
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

// A unit's guard is asked after the gap, right before the next part: false
// drops the Enter, and no terminal-query reply is squeezed in during the gap.
func TestPTYWriter_GuardBeforeEnterAndNoReplyInTheGap(t *testing.T) {
	rec := &recorder{}
	done := make(chan struct{})
	defer close(done)
	pw := newPTYWriter(rec, done, nil)
	uw := unitWriter(pw)
	ctx := context.Background()

	var replyAccepted atomic.Bool
	calls := 0
	sent, err := uw(ctx, WriteUnit{Parts: [][]byte{[]byte("go on"), {'\r'}}, Gap: 80 * time.Millisecond, Wait: true,
		Guard: func() bool {
			calls++
			return false
		}})
	require.NoError(t, err)
	require.False(t, sent, "the guard dropped the Enter")
	require.Equal(t, 1, calls)
	require.Equal(t, []string{"go on"}, rec.got(), "the text stays typed, no Enter")
	require.Eventually(t, func() bool { return !pw.busy.Load() }, time.Second, 5*time.Millisecond)

	// A terminal query arrives during the gap of the next unit: it is dropped.
	rec2 := &recorder{}
	pw2 := newPTYWriter(rec2, done, nil)
	uw2 := unitWriter(pw2)
	go func() {
		time.Sleep(30 * time.Millisecond) // inside the 80 ms gap
		replyAccepted.Store(pw2.TrySendReply([]byte("\x1b[?1;2c")))
	}()
	sent, err = uw2(ctx, WriteUnit{Parts: [][]byte{[]byte("go on"), {'\r'}}, Gap: 80 * time.Millisecond, Wait: true,
		Guard: func() bool { return true }})
	require.NoError(t, err)
	require.True(t, sent)
	require.False(t, replyAccepted.Load(), "a reply never lands between the text and its Enter")
	require.Equal(t, []string{"go on", "\r"}, rec2.got())

	// Without Wait the unit is only queued.
	sent, err = uw2(ctx, WriteUnit{Parts: [][]byte{[]byte("x")}})
	require.NoError(t, err)
	require.True(t, sent)
	require.Eventually(t, func() bool { return len(rec2.got()) == 3 }, time.Second, 5*time.Millisecond)
}

func TestUnitWriter_ClosedWriter(t *testing.T) {
	rec := &recorder{}
	done := make(chan struct{})
	pw := newPTYWriter(rec, done, nil)
	uw := unitWriter(pw)
	close(done)
	_, err := uw(context.Background(), WriteUnit{Parts: [][]byte{[]byte("x")}, Wait: true})
	require.Error(t, err)
}

// End to end through a real supervisor: an OSC 7501 status query arrives from
// the agent in the 120 ms gap between a loop's text and its Enter. The writer
// is busy with the unit, so the reply is dropped: the child reads exactly
// "go on" then Enter, with no reply bytes in between.
func TestSupervisor_OSCQueryInTheGapNeverLandsBetweenTextAndEnter(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses bash and a Unix tty")
	}
	h, err := adapter.NewHeuristic(`x`, "", 0)
	require.NoError(t, err)
	stateDir := t.TempDir()
	store := state.NewStore()
	sess := &state.Session{ID: "gap1", ShortID: "gap1", ToolID: "qwen", Slug: "gap",
		State: protocol.StateQueued, StartedAt: time.Now().UTC()}
	require.NoError(t, store.Add(sess))

	var uw atomic.Pointer[UnitWriter]
	// Raw tty: read the 5 typed characters, ask a status query, then echo
	// whatever else arrives so the transcript shows the order of the input.
	script := `stty raw -echo; read -r -n5 x; printf '\033]7501;?\007'; cat`
	sup := New(SupervisorConfig{
		StateDir: stateDir, Store: store, Command: []string{"bash", "-c", script},
		Adapter: adapter.NewOSCStatus(h), IdleTick: 20 * time.Millisecond,
		RegisterWriter:   func(w UnitWriter) { uw.Store(&w) },
		UnregisterWriter: func() {},
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runDone := make(chan error, 1)
	go func() { runDone <- sup.Run(ctx, sess) }()

	require.Eventually(t, func() bool { return uw.Load() != nil }, 10*time.Second, 10*time.Millisecond)
	time.Sleep(300 * time.Millisecond) // the shell is at its read
	sent, err := (*uw.Load())(ctx, WriteUnit{Parts: [][]byte{[]byte("go on"), {'\r'}}, Gap: 120 * time.Millisecond,
		Wait: true, Guard: func() bool { return true }})
	require.NoError(t, err)
	require.True(t, sent)

	require.Eventually(t, func() bool {
		tail, _ := state.TranscriptTail(stateDir, "gap1", 1<<16)
		return bytes.Contains(tail, []byte("\r"))
	}, 10*time.Second, 20*time.Millisecond)
	time.Sleep(300 * time.Millisecond) // a late reply would show up by now
	tail, _ := state.TranscriptTail(stateDir, "gap1", 1<<16)
	// The child's own query is the one OSC 7501 sequence; a reply written to
	// its stdin would be echoed back as a second one.
	require.Equal(t, 1, bytes.Count(tail, []byte("]7501;")), "no reply reached the child: %q", tail)
	cancel()
	<-runDone
}

// A caller that gave up (its timeout passed) never has its unit written
// later: not when it was still queued behind a stalled write, and not the
// Enter of a unit whose gap outlived the caller.
func TestUnitWriter_AbandonedUnitIsNeverWritten(t *testing.T) {
	rec := &recorder{block: make(chan struct{})}
	done := make(chan struct{})
	defer close(done)
	pw := newPTYWriter(rec, done, nil)
	uw := unitWriter(pw)

	// The child stalls stdin: the first write blocks.
	require.True(t, pw.Send(context.Background(), writeUnit{parts: [][]byte{[]byte("stuck")}}))
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := uw(ctx, WriteUnit{Parts: [][]byte{[]byte("late"), {'\r'}}, Gap: 10 * time.Millisecond, Wait: true})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	close(rec.block) // the child reads again
	time.Sleep(200 * time.Millisecond)
	require.Equal(t, []string{"stuck"}, rec.got(), "the abandoned unit was skipped")

	// Mid-unit: the caller's context ends during the gap, so no Enter.
	rec2 := &recorder{}
	pw2 := newPTYWriter(rec2, done, nil)
	uw2 := unitWriter(pw2)
	ctx2, cancel2 := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel2()
	sent, err := uw2(ctx2, WriteUnit{Parts: [][]byte{[]byte("go on"), {'\r'}}, Gap: 300 * time.Millisecond, Wait: true})
	require.False(t, sent)
	require.Error(t, err)
	time.Sleep(400 * time.Millisecond)
	require.Equal(t, []string{"go on"}, rec2.got(), "text typed, Enter never sent")
}

// The writer ending during the gap is an error (a loop pauses as send_failed),
// not a guard refusal.
func TestUnitWriter_WriterEndingInTheGapIsAnError(t *testing.T) {
	rec := &recorder{}
	done := make(chan struct{})
	pw := newPTYWriter(rec, done, nil)
	uw := unitWriter(pw)
	go func() { time.Sleep(60 * time.Millisecond); close(done) }()
	sent, err := uw(context.Background(), WriteUnit{Parts: [][]byte{[]byte("a"), {'\r'}}, Gap: 500 * time.Millisecond, Wait: true})
	require.False(t, sent)
	require.ErrorIs(t, err, errInputClosed)
}
