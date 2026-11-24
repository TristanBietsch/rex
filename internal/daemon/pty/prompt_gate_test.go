package pty

import (
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// gateAt runs g.step for a sample taken at start+at.
func gateAt(g *promptGate, start time.Time, at time.Duration, s promptSample) promptDecision {
	return g.step(start.Add(at), s)
}

func testGate(start time.Time) *promptGate {
	return newPromptGate(promptGateConfig{
		ReadyRe:  regexp.MustCompile(`(?m)^> $`),
		TrustRe:  regexp.MustCompile(`trust this folder`),
		Settle:   100 * time.Millisecond,
		Deadline: time.Second,
		HardCap:  10 * time.Second,
	}, start)
}

func TestPromptGate_WaitsForReadyRegex(t *testing.T) {
	start := time.Now()
	g := testGate(start)
	require.Equal(t, promptWait, gateAt(g, start, 100*time.Millisecond, promptSample{Screen: "loading…", Idle: time.Second}))
	require.Equal(t, promptPaste, gateAt(g, start, 200*time.Millisecond, promptSample{Screen: "> \n", Idle: time.Second}))
}

func TestPromptGate_AdapterReadyAlsoCounts(t *testing.T) {
	start := time.Now()
	g := newPromptGate(promptGateConfig{
		AdapterReady: func([]byte, time.Duration) bool { return true },
		Settle:       0, Deadline: time.Second, HardCap: 10 * time.Second,
	}, start)
	require.Equal(t, promptPaste, gateAt(g, start, 0, promptSample{Screen: "anything"}))
}

// Ready and the trust dialog both on screen: never paste (an Enter would
// answer the dialog). Once the dialog is gone, paste.
func TestPromptGate_NeverIntoTrustDialog(t *testing.T) {
	start := time.Now()
	g := testGate(start)
	both := promptSample{Screen: "Do you trust this folder?\n> \n", Idle: time.Second}
	require.Equal(t, promptWait, gateAt(g, start, 100*time.Millisecond, both))
	require.Equal(t, promptPaste, gateAt(g, start, 300*time.Millisecond, promptSample{Screen: "> \n", Idle: time.Second}))
}

// The deadline counts only time with no dialog on screen: a dialog open for
// three times the deadline, then answered, still gets the prompt.
func TestPromptGate_DeadlinePausedWhileDialogShown(t *testing.T) {
	start := time.Now()
	g := testGate(start)
	dialog := promptSample{Screen: "trust this folder", Idle: time.Second}
	for at := 200 * time.Millisecond; at <= 3*time.Second; at += 200 * time.Millisecond {
		require.Equal(t, promptWait, gateAt(g, start, at, dialog), at)
	}
	require.Equal(t, promptPaste, gateAt(g, start, 3200*time.Millisecond, promptSample{Screen: "> \n", Idle: time.Second}))
}

func TestPromptGate_WaitsForSettle(t *testing.T) {
	start := time.Now()
	g := testGate(start)
	require.Equal(t, promptWait, gateAt(g, start, 100*time.Millisecond, promptSample{Screen: "> \n", Idle: 50 * time.Millisecond}))
	require.Equal(t, promptPaste, gateAt(g, start, 200*time.Millisecond, promptSample{Screen: "> \n", Idle: 150 * time.Millisecond}))
}

func TestPromptGate_GivesUpAfterDeadline(t *testing.T) {
	start := time.Now()
	g := testGate(start)
	require.Equal(t, promptWait, gateAt(g, start, 500*time.Millisecond, promptSample{Screen: "loading"}))
	require.Equal(t, promptGiveUp, gateAt(g, start, 1100*time.Millisecond, promptSample{Screen: "loading"}))
}

// The hard cap bounds the wait even while a dialog keeps the deadline paused.
func TestPromptGate_HardCapEndsADialogWait(t *testing.T) {
	start := time.Now()
	g := testGate(start)
	dialog := promptSample{Screen: "trust this folder"}
	require.Equal(t, promptWait, gateAt(g, start, 9*time.Second, dialog))
	require.Equal(t, promptGiveUp, gateAt(g, start, 10*time.Second, dialog))
}

func TestPromptGate_DefaultsAndNoSignal(t *testing.T) {
	g := newPromptGate(promptGateConfig{ReadyRe: regexp.MustCompile(`x`)}, time.Now())
	require.Equal(t, promptReadyDeadline, g.cfg.Deadline)
	require.Equal(t, promptHardCap, g.cfg.HardCap)
	require.False(t, promptGateConfig{}.hasSignal())
	require.True(t, promptGateConfig{ReadyRe: regexp.MustCompile(`x`)}.hasSignal())
}
