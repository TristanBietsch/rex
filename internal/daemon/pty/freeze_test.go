//go:build !windows

package pty

import (
	"bytes"
	"context"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// syncBuf collects OutputSink chunks.
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) add(p []byte) { s.mu.Lock(); s.b.Write(p); s.mu.Unlock() }
func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func newTestSession(t *testing.T, store *state.Store, id string) *state.Session {
	t.Helper()
	sess := &state.Session{ID: id, ShortID: id, ToolID: "echo", Slug: id, State: protocol.StateQueued, StartedAt: time.Now().UTC()}
	require.NoError(t, store.Add(sess))
	return sess
}

// A freeze parks reader, writer and input: input sent and output produced
// while frozen are neither lost nor seen until the thaw, and then exactly
// once.
func TestFreezeParksReaderAndWriter(t *testing.T) {
	store := state.NewStore()
	sess := newTestSession(t, store, "frz1")
	out := &syncBuf{}
	input := make(chan []byte, 16)
	var freeze FreezeFunc
	registered := make(chan struct{})
	sup := New(SupervisorConfig{
		StateDir: t.TempDir(), Store: store,
		Command:    []string{"sh", "-c", "stty -echo; echo READY; cat"},
		InputCh:    input,
		OutputSink: out.add,
		RegisterFreeze: func(f FreezeFunc) {
			freeze = f
			close(registered)
		},
		IdleTick: 20 * time.Millisecond,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- sup.Run(ctx, sess) }()
	<-registered
	require.Eventually(t, func() bool { return strings.Contains(out.String(), "READY") }, 3*time.Second, 10*time.Millisecond)

	input <- []byte("before\n")
	require.Eventually(t, func() bool { return strings.Contains(out.String(), "before") }, 3*time.Second, 10*time.Millisecond)

	fz, thaw, err := freeze(context.Background())
	require.NoError(t, err)
	require.Positive(t, fz.PID)
	require.Equal(t, uint16(120), fz.Cols)
	require.Contains(t, string(fz.Screen), "before")

	// Typed while frozen: the writer is parked, nothing reaches cat.
	input <- []byte("during\n")
	time.Sleep(300 * time.Millisecond)
	require.NotContains(t, out.String(), "during")

	thaw()
	require.Eventually(t, func() bool { return strings.Contains(out.String(), "during") }, 3*time.Second, 10*time.Millisecond)
	require.Equal(t, 1, strings.Count(out.String(), "during"))
	require.Equal(t, 1, strings.Count(out.String(), "before"))

	cancel()
	<-done
}

// Output the agent writes while the session is frozen stays in the kernel
// until the thaw: the reader never holds a chunk across a freeze.
func TestFreezeHoldsAgentOutputInKernel(t *testing.T) {
	store := state.NewStore()
	sess := newTestSession(t, store, "frz2")
	out := &syncBuf{}
	var freeze FreezeFunc
	registered := make(chan struct{})
	sup := New(SupervisorConfig{
		StateDir: t.TempDir(), Store: store,
		// Prints a numbered line every 20 ms.
		Command:        []string{"sh", "-c", "i=0; while [ $i -lt 100 ]; do echo line$i; i=$((i+1)); sleep 0.02; done"},
		OutputSink:     out.add,
		RegisterFreeze: func(f FreezeFunc) { freeze = f; close(registered) },
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- sup.Run(ctx, sess) }()
	<-registered
	require.Eventually(t, func() bool { return strings.Contains(out.String(), "line3") }, 3*time.Second, 5*time.Millisecond)
	_, thaw, err := freeze(context.Background())
	require.NoError(t, err)
	frozenAt := out.String()
	time.Sleep(200 * time.Millisecond)
	require.Equal(t, frozenAt, out.String(), "no output may be handled while frozen")
	thaw()
	require.NoError(t, <-done)
	all := out.String()
	for i := range 100 {
		require.Equal(t, 1, strings.Count(all, "line"+strconv.Itoa(i)+"\r\n"), "line%d", i)
	}
}

// A child started elsewhere (the previous daemon image) is supervised in
// place: input reaches it, output comes back, the screen starts from the
// handed-over bytes, and a complete reaps it as done.
func TestAdoptRunsExistingChild(t *testing.T) {
	cmd := exec.Command("sh", "-c", "stty -echo; cat")
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: 80, Rows: 24})
	require.NoError(t, err)
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	store := state.NewStore()
	sess := newTestSession(t, store, "adopt1")
	out := &syncBuf{}
	input := make(chan []byte, 4)
	complete := make(chan struct{}, 1)
	var screen string
	var attach func(fn func(func() []byte))
	ready := make(chan struct{})
	sup := New(SupervisorConfig{
		StateDir: t.TempDir(), Store: store,
		Command:     []string{"sh"},
		InputCh:     input,
		CompleteCh:  complete,
		OutputSink:  out.add,
		InitialCols: 80, InitialRows: 24,
		Adopt: &Adopted{File: f, PID: cmd.Process.Pid, Screen: []byte("\x1b[1;1Hhanded-over")},
		RegisterAttach: func(fn func(fn func(repaint func() []byte))) {
			attach = fn
			close(ready)
		},
	})
	done := make(chan error, 1)
	go func() { done <- sup.Run(context.Background(), sess) }()
	<-ready
	attach(func(repaint func() []byte) { screen = string(repaint()) })
	require.Contains(t, screen, "handed-over")

	input <- []byte("ping\n")
	require.Eventually(t, func() bool { return strings.Contains(out.String(), "ping") }, 3*time.Second, 10*time.Millisecond)
	complete <- struct{}{}
	require.NoError(t, <-done)
	got, _ := store.Get("adopt1")
	require.Equal(t, protocol.StateDone, got.State)
	// Reaped: no such process left.
	require.Error(t, unix.Kill(cmd.Process.Pid, 0))
}

// A handed-over child that exited during the exec ends the session at
// once, with its exit status.
func TestAdoptExitedChild(t *testing.T) {
	cmd := exec.Command("sh", "-c", "exit 3")
	f, err := pty.Start(cmd)
	require.NoError(t, err)
	// Let it exit (not reaped: Wait comes from the supervisor).
	time.Sleep(200 * time.Millisecond)

	store := state.NewStore()
	sess := newTestSession(t, store, "adopt2")
	sup := New(SupervisorConfig{
		StateDir: t.TempDir(), Store: store, Command: []string{"sh"},
		Adopt: &Adopted{File: f, PID: cmd.Process.Pid},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = sup.Run(ctx, sess)
	got, _ := store.Get("adopt2")
	require.Equal(t, protocol.StateFailed, got.State)
	require.NotNil(t, got.ExitCode)
	require.Equal(t, 3, *got.ExitCode)
}

// A session whose initial prompt is not typed yet refuses a freeze.
func TestFreezeRefusedWhileStarting(t *testing.T) {
	store := state.NewStore()
	sess := newTestSession(t, store, "frz3")
	var freeze FreezeFunc
	registered := make(chan struct{})
	sup := New(SupervisorConfig{
		StateDir: t.TempDir(), Store: store,
		Command:        []string{"sh", "-c", "cat"},
		Adapter:        neverReady{},
		InitialPrompt:  "hello",
		RegisterFreeze: func(f FreezeFunc) { freeze = f; close(registered) },
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- sup.Run(ctx, sess) }()
	<-registered
	_, _, err := freeze(context.Background())
	require.ErrorIs(t, err, errStarting)
	cancel()
	<-done
}

type neverReady struct{}

func (neverReady) Detect([]byte, time.Duration) protocol.State { return "" }
func (neverReady) IsReadyForInput([]byte, time.Duration) bool  { return false }
