package pty

import (
	"regexp"
	"time"
)

const (
	// promptReadyDeadline is how long the first prompt waits for the agent to
	// be ready. It counts only time with no trust dialog on screen, so a user
	// who answers the dialog minutes later still gets the prompt delivered.
	promptReadyDeadline = 30 * time.Second
	// promptHardCap bounds the whole wait, dialog time included.
	promptHardCap = 10 * time.Minute
)

// promptGateConfig says when the first prompt may be pasted.
type promptGateConfig struct {
	// AdapterReady is the agent adapter's readiness check (nil: none).
	AdapterReady func(window []byte, idle time.Duration) bool
	// ReadyRe matches the rendered screen once the agent waits for input
	// (the tool's prompt_paste.ready_regex; nil: none).
	ReadyRe *regexp.Regexp
	// TrustRe matches the rendered screen while the agent's folder-trust
	// dialog is shown (its trust_prompt.shown; nil: none). The prompt is
	// never pasted while it matches: its Enter would answer the dialog.
	TrustRe *regexp.Regexp
	// Settle is how long the output must be quiet before the paste.
	Settle time.Duration
	// Deadline and HardCap override promptReadyDeadline and promptHardCap
	// (zero: the defaults).
	Deadline, HardCap time.Duration
}

// hasSignal reports whether anything can say the agent is ready. Without
// one the prompt is never pasted.
func (c promptGateConfig) hasSignal() bool { return c.AdapterReady != nil || c.ReadyRe != nil }

// promptSample is what the gate looks at on each poll.
type promptSample struct {
	Window []byte        // the recent raw output, for the adapter
	Idle   time.Duration // time since the last output
	Screen string        // the rendered screen text
}

type promptDecision int

const (
	promptWait promptDecision = iota
	promptPaste
	promptGiveUp
)

// promptGate decides, poll by poll, whether to paste the first prompt now,
// keep waiting, or give up. It is not safe for concurrent use; the paste
// goroutine owns it.
type promptGate struct {
	cfg     promptGateConfig
	start   time.Time
	last    time.Time
	waited  time.Duration // time with no dialog on screen
	pausing bool          // the dialog was on screen at the last poll
}

func newPromptGate(cfg promptGateConfig, start time.Time) *promptGate {
	if cfg.Deadline <= 0 {
		cfg.Deadline = promptReadyDeadline
	}
	if cfg.HardCap <= 0 {
		cfg.HardCap = promptHardCap
	}
	return &promptGate{cfg: cfg, start: start, last: start}
}

// step takes one sample at now. The time since the previous step counts
// against the deadline unless the dialog was on screen through it.
func (g *promptGate) step(now time.Time, s promptSample) promptDecision {
	dialog := g.cfg.TrustRe != nil && g.cfg.TrustRe.MatchString(s.Screen)
	if !g.pausing && !dialog {
		g.waited += now.Sub(g.last)
	}
	g.last, g.pausing = now, dialog
	if now.Sub(g.start) >= g.cfg.HardCap || g.waited >= g.cfg.Deadline {
		return promptGiveUp
	}
	if dialog || s.Idle < g.cfg.Settle {
		return promptWait
	}
	ready := g.cfg.ReadyRe != nil && g.cfg.ReadyRe.MatchString(s.Screen)
	if !ready && g.cfg.AdapterReady != nil {
		ready = g.cfg.AdapterReady(s.Window, s.Idle)
	}
	if ready {
		return promptPaste
	}
	return promptWait
}
