package pty

import (
	"bytes"
	"context"
	"log/slog"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func TestUnansweredDetector(t *testing.T) {
	shown := regexp.MustCompile(`Yes, I trust this folder`)
	t0 := time.Unix(1000, 0)
	d := newUnansweredDetector(shown, t0)

	require.False(t, d.next("loading…", t0.Add(time.Second)), "no dialog yet")
	require.True(t, d.next(dialogNo, t0.Add(2*time.Second)), "reported when the dialog shows")
	require.False(t, d.next(dialogNo, t0.Add(3*time.Second)), "once only")

	late := newUnansweredDetector(shown, t0)
	require.False(t, late.next(dialogNo, t0.Add(trustWindow+time.Second)), "after the window it is never wanted")
	require.False(t, late.next(dialogNo, t0.Add(trustWindow+2*time.Second)))

	require.Nil(t, newUnansweredDetector(nil, t0))
	var none *unansweredDetector
	require.False(t, none.next(dialogNo, t0))
}

// lockedBuffer is a bytes.Buffer safe for the supervisor goroutine and the
// test to share.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func TestUnansweredTrustDialogLoggedOnce(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake dialog needs bash and stty")
	}
	var logs lockedBuffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(old) })

	stateDir := t.TempDir()
	store := state.NewStore()
	sess := &state.Session{ID: "trustlog1", ShortID: "trustlog1", ToolID: "fake", Slug: "trustlog",
		State: protocol.StateQueued, StartedAt: time.Now().UTC()}
	require.NoError(t, store.Add(sess))
	sup := New(SupervisorConfig{
		StateDir: stateDir, Store: store,
		Command:     []string{"bash", "-c", fakeTrustDialog},
		IdleTick:    50 * time.Millisecond,
		TrustDetect: testTrust().Shown,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	_ = sup.Run(ctx, sess)

	got := logs.String()
	require.Equal(t, 1, strings.Count(got, "trust dialog shown; folder not trusted"), got)
	require.Contains(t, got, "rex trust")
	require.Contains(t, got, "trustlog1")
}

func TestArmedTrustDialogIsNotLoggedAsUnanswered(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake dialog needs bash and stty")
	}
	var logs lockedBuffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(old) })

	stateDir := t.TempDir()
	store := state.NewStore()
	sess := &state.Session{ID: "trustlog2", ShortID: "trustlog2", ToolID: "fake", Slug: "trustlog2",
		State: protocol.StateQueued, StartedAt: time.Now().UTC()}
	require.NoError(t, store.Add(sess))
	sup := New(SupervisorConfig{
		StateDir: stateDir, Store: store,
		Command:     []string{"bash", "-c", fakeTrustDialog},
		IdleTick:    50 * time.Millisecond,
		TrustPrompt: testTrust(),
		TrustDetect: testTrust().Shown,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	_ = sup.Run(ctx, sess)
	require.NotContains(t, logs.String(), "folder not trusted")
}
