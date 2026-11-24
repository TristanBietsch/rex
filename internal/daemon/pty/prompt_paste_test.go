package pty

import (
	"context"
	"regexp"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// runPromptChild runs a scripted child under a supervisor with cfg (Store,
// StateDir and Command filled in) for d, then returns the transcript.
func runPromptChild(t *testing.T, script string, d time.Duration, cfg SupervisorConfig) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("reads the paste back through Unix PTY input echo")
	}
	stateDir := t.TempDir()
	store := state.NewStore()
	sess := &state.Session{ID: "p1", ShortID: "p1", ToolID: "echo", Slug: "test",
		State: protocol.StateQueued, StartedAt: time.Now().UTC()}
	require.NoError(t, store.Add(sess))
	cfg.StateDir, cfg.Store = stateDir, store
	cfg.Command = []string{"bash", "-c", script}
	if cfg.IdleTick == 0 {
		cfg.IdleTick = 50 * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- New(cfg).Run(ctx, sess) }()
	<-done
	tail, err := state.TranscriptTail(stateDir, sess.ID, 1<<16)
	require.NoError(t, err)
	return string(tail)
}

// No adapter, but the tool's ready_regex matches the rendered screen once
// the child draws its prompt: the prompt goes in then, not before.
func TestSupervisor_PromptWaitsForReadyRegex(t *testing.T) {
	out := runPromptChild(t, `echo loading; sleep 0.6; echo MARK; printf 'READY> '; sleep 3`, 2*time.Second, SupervisorConfig{
		InitialPrompt: "hello-ready", ReadyRe: regexp.MustCompile(`READY>`),
	})
	i, j := strings.Index(out, "MARK"), strings.Index(out, "hello-ready")
	require.GreaterOrEqual(t, i, 0, out)
	require.Greater(t, j, i, "pasted only after the ready prompt was drawn: %q", out)
}

// The ready prompt and the trust dialog are both on screen: nothing is
// typed. Once a redraw removes the dialog from the screen, one paste.
func TestSupervisor_PromptNeverTypedIntoTrustDialog(t *testing.T) {
	script := `printf 'Do you trust this folder?\nREADY> '; sleep 1.2; printf '\033[2J\033[H'; echo CLEARED; printf 'READY> '; sleep 3`
	out := runPromptChild(t, script, 2500*time.Millisecond, SupervisorConfig{
		InitialPrompt: "hello-dialog", ReadyRe: regexp.MustCompile(`READY>`),
		TrustDetect: regexp.MustCompile(`trust this folder`),
	})
	i, j := strings.Index(out, "CLEARED"), strings.Index(out, "hello-dialog")
	require.GreaterOrEqual(t, i, 0, out)
	require.Greater(t, j, i, "never pasted while the dialog showed: %q", out)
	require.Equal(t, 1, strings.Count(out, "hello-dialog"), "pasted once")
}

// The deadline counts only time with no dialog shown: a dialog open for
// longer than the deadline, then closed, still gets the prompt.
func TestSupervisor_PromptDeadlinePausedWhileDialogShown(t *testing.T) {
	script := `printf 'Do you trust this folder?'; sleep 1.5; printf '\033[2J\033[H'; echo CLEARED; printf 'READY> '; sleep 3`
	var undelivered atomic.Int32
	out := runPromptChild(t, script, 3*time.Second, SupervisorConfig{
		InitialPrompt: "hello-late", ReadyRe: regexp.MustCompile(`READY>`),
		TrustDetect:         regexp.MustCompile(`trust this folder`),
		PromptDeadline:      700 * time.Millisecond,
		OnPromptUndelivered: func() { undelivered.Add(1) },
	})
	require.Contains(t, out, "hello-late")
	require.Zero(t, undelivered.Load())
}

// Never ready: the callback fires once and nothing is typed.
func TestSupervisor_PromptUndeliveredCallsBack(t *testing.T) {
	var undelivered atomic.Int32
	out := runPromptChild(t, `echo loading; sleep 3`, 1500*time.Millisecond, SupervisorConfig{
		InitialPrompt: "hello-never", ReadyRe: regexp.MustCompile(`READY>`),
		PromptDeadline:      400 * time.Millisecond,
		OnPromptUndelivered: func() { undelivered.Add(1) },
	})
	require.NotContains(t, out, "hello-never")
	require.Equal(t, int32(1), undelivered.Load())
}
