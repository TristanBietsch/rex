package pty

import (
	"regexp"
	"time"
)

// TrustAnswer accepts an agent's folder-trust dialog by keyboard: while
// Shown matches the screen, send Move until Selected matches, then Confirm.
// The server sets it only for sessions under auto_trust_dirs.
type TrustAnswer struct {
	Shown    *regexp.Regexp // the dialog is on screen
	Selected *regexp.Regexp // the accept option is the selected one
	Move     []byte         // moves the selection one step
	Confirm  []byte         // confirms the selection
}

const (
	// trustStep is the pause between keys, so the agent redraws first.
	trustStep = 300 * time.Millisecond
	// trustMaxMoves bounds Move presses; the accept option is never far.
	trustMaxMoves = 4
	// trustWindow is how long after spawn the dialog is looked for. It
	// appears at startup; matching later output is never wanted.
	trustWindow = 60 * time.Second
)

// trustAnswerer is one session's progress through the dialog. It finishes
// after confirming, after trustMaxMoves moves without reaching the accept
// option, or once trustWindow has passed.
type trustAnswerer struct {
	a       *TrustAnswer
	started time.Time
	last    time.Time
	moves   int
	done    bool
}

func newTrustAnswerer(a *TrustAnswer, now time.Time) *trustAnswerer {
	if a == nil || a.Shown == nil || a.Selected == nil || len(a.Move) == 0 || len(a.Confirm) == 0 {
		return nil
	}
	return &trustAnswerer{a: a, started: now}
}

// next returns the keys to send for the current screen text and a label
// for the log, or nil when there is nothing to do now.
func (t *trustAnswerer) next(screen string, now time.Time) ([]byte, string) {
	if t == nil || t.done {
		return nil, ""
	}
	if now.Sub(t.started) > trustWindow {
		t.done = true
		return nil, "window passed"
	}
	if !t.a.Shown.MatchString(screen) || now.Sub(t.last) < trustStep {
		return nil, ""
	}
	t.last = now
	if t.a.Selected.MatchString(screen) {
		t.done = true
		return t.a.Confirm, "confirm"
	}
	if t.moves >= trustMaxMoves {
		t.done = true
		return nil, "gave up: accept option never selected"
	}
	t.moves++
	return t.a.Move, "move"
}
