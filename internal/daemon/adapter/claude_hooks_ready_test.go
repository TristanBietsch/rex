package adapter

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// The tests in this file replay hook files as Claude Code writes them (the
// lines each hook prints, in order) and check when the session is READY
// (needs_input). READY means the agent waits on the user: a question, or a
// finished turn with no subagent and no tool still running.

// replay appends hook lines, one per element; an element may hold several
// lines (one hook's printf).
func replay(t *testing.T, path string, lines ...string) {
	t.Helper()
	for _, l := range lines {
		appendLine(t, path, l)
	}
}

// readyEdges ticks Detect like the supervisor does and counts
// working→needs_input edges.
type readyEdges struct {
	a     *ClaudeHooks
	last  protocol.State
	edges int
}

func (r *readyEdges) tick(idle time.Duration) protocol.State {
	st := r.a.Detect(nil, idle)
	if r.last == protocol.StateWorking && st == protocol.StateNeedsInput {
		r.edges++
	}
	r.last = st
	return st
}

// (i) A foreground subagent (main-thread Task call) runs while the main
// agent's screen stays still: no READY, the session waits on the subagent.
func TestClaudeHooks_ForegroundSubagentStillScreenStaysWorking(t *testing.T) {
	a, path, now := waitingAdapter(t)
	replay(t, path,
		"working\nwait-", // UserPromptSubmit
		"working\nwait+ subagent",
		"agent+ sub-1",
	)
	for _, idle := range []time.Duration{0, 15 * time.Second, 20 * time.Second, time.Minute, 10 * time.Minute} {
		require.Equal(t, protocol.StateWorking, a.Detect(nil, idle), "idle %s", idle)
		kind, _ := a.Waiting()
		require.Equal(t, protocol.WaitSubagent, kind, "idle %s", idle)
	}
	// The subagent's own tool calls keep it alive and the session working.
	*now = now.Add(5 * time.Minute)
	replay(t, path, "working", "working\nagent~ sub-1")
	require.Equal(t, protocol.StateWorking, a.Detect(nil, 30*time.Second))
	kind, _ := a.Waiting()
	require.Equal(t, protocol.WaitSubagent, kind)
}

// (ii) The main turn ends (Stop) while background subagents still run: the
// session keeps working, waiting on a subagent, with no wait reason.
func TestClaudeHooks_StopWithBackgroundSubagentsIsWaiting(t *testing.T) {
	a, path, now := waitingAdapter(t)
	replay(t, path,
		"working\nwait-",
		"working\nwait+ subagent",
		"agent+ bg-1",
		"working\nwait-", // the Task call returns at once (run_in_background)
		"needs_input\nturn-done",
	)
	stopAt := *now
	r := &readyEdges{a: a, last: protocol.StateWorking}
	for _, idle := range []time.Duration{0, 20 * time.Second, 2 * time.Minute} {
		require.Equal(t, protocol.StateWorking, r.tick(idle))
		kind, since := a.Waiting()
		require.Equal(t, protocol.WaitSubagent, kind)
		require.Equal(t, stopAt, since)
		require.Empty(t, a.WaitReason(), "no reason while it works")
	}
	// An idle_prompt notification does not make it READY either.
	replay(t, path, "needs_input\nnotify idle_prompt")
	require.Equal(t, protocol.StateWorking, r.tick(time.Minute))
	require.Zero(t, r.edges)
}

// (iii) Each subagent completion wakes the main agent; only the Stop after
// the last one is READY, exactly once.
func TestClaudeHooks_SubagentWakeThenStopReadyOnce(t *testing.T) {
	a, path, now := waitingAdapter(t)
	r := &readyEdges{a: a, last: protocol.StateWorking}
	step := func(lines ...string) {
		replay(t, path, lines...)
		*now = now.Add(time.Second)
		r.tick(0)
		r.tick(20 * time.Second) // a still screen between hooks
	}
	step("working\nwait-")
	step("working\nwait+ subagent", "agent+ a")
	step("working\nwait-")
	step("working\nwait+ subagent", "agent+ b")
	step("working\nwait-")
	step("needs_input\nturn-done") // main turn ends, a and b run on
	step("working", "working\nagent~ a")
	step("agent- a")               // a ends; the main agent wakes
	step("working\nwait-")         // it reads a's result
	step("needs_input\nturn-done") // and stops again, b still running
	step("agent- b")               // b ends; the main agent wakes
	require.Zero(t, r.edges, "no READY while a subagent runs or right after one ends")
	step("working\nwait-")
	step("needs_input\nturn-done") // last Stop, nothing running
	require.Equal(t, protocol.StateNeedsInput, r.last)
	require.Equal(t, 1, r.edges, "READY exactly once")
	require.Equal(t, protocol.WaitReview, a.WaitReason())
	kind, _ := a.Waiting()
	require.Empty(t, kind)
}

// The quiet timeout ends a parked wait whose SubagentStop was lost: after
// subagentQuiet plus the wake grace, the session is READY.
func TestClaudeHooks_QuietSubagentEndsParkedWait(t *testing.T) {
	a, path, now := waitingAdapter(t)
	replay(t, path, "agent+ lost", "needs_input\nturn-done")
	require.Equal(t, protocol.StateWorking, a.Detect(nil, time.Minute))
	*now = now.Add(subagentQuiet + time.Second)
	require.Equal(t, protocol.StateWorking, a.Detect(nil, time.Minute), "the main agent may still wake")
	*now = now.Add(wakeGrace)
	require.Equal(t, protocol.StateNeedsInput, a.Detect(nil, time.Minute))
	kind, _ := a.Waiting()
	require.Empty(t, kind)
	require.Equal(t, protocol.WaitReview, a.WaitReason())
}

// A question is READY at once, even from a subagent while others run.
func TestClaudeHooks_QuestionReadyWithSubagentsRunning(t *testing.T) {
	a, path, _ := waitingAdapter(t)
	replay(t, path, "agent+ a", "needs_input\nturn-done", "needs_input\nnotify permission_prompt")
	require.Equal(t, protocol.StateNeedsInput, a.Detect(nil, 0))
	require.Equal(t, protocol.WaitInput, a.WaitReason())
	// The answer lets the subagent go on: waiting on it again.
	replay(t, path, "working\nagent~ a")
	require.Equal(t, protocol.StateWorking, a.Detect(nil, time.Minute))
	kind, _ := a.Waiting()
	require.Equal(t, protocol.WaitSubagent, kind)
}

// The stale fallback (a turn interrupted with Esc fires no Stop) waits
// longer, and much longer with a tool wait open.
func TestClaudeHooks_StaleFallbackBounds(t *testing.T) {
	a, path, _ := waitingAdapter(t)
	replay(t, path, "working\nwait-")
	require.Equal(t, protocol.StateWorking, a.Detect(nil, 15*time.Second))
	require.Equal(t, protocol.StateNeedsInput, a.Detect(nil, hookStaleWorking))

	b, pathB, _ := waitingAdapter(t)
	replay(t, pathB, "working\nwait+ shell") // Esc during a Bash call
	require.Equal(t, protocol.StateWorking, b.Detect(nil, 5*time.Minute))
	require.Equal(t, protocol.StateNeedsInput, b.Detect(nil, subagentQuiet))
	kind, _ := b.Waiting()
	require.Empty(t, kind)
}

// Each UserPromptSubmit counts a prompt; a truncated file never lowers it.
func TestClaudeHooks_PromptCounter(t *testing.T) {
	a, path, _ := waitingAdapter(t)
	var _ PromptCounter = a
	require.Zero(t, a.Prompts())
	runHook(t, hookCommands(t)["UserPromptSubmit"], path, `{"session_id":"s","prompt":"hi"}`)
	require.Equal(t, 1, a.Prompts())
	replay(t, path, "working\nwait-\nprompt")
	require.Equal(t, 2, a.Prompts())
	truncate(t, path)
	replay(t, path, "working\nwait-\nprompt")
	require.Equal(t, 3, a.Prompts())
}

func truncate(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, nil, 0o600))
}

// A subagent's own working words while parked, then its stop: if the main
// agent does not wake within wakeGrace, the finished turn is READY for
// review.
func TestClaudeHooks_ParkedSubagentWorkingThenMissedWake(t *testing.T) {
	a, path, now := waitingAdapter(t)
	replay(t, path, "agent+ a", "needs_input\nturn-done", "working", "working\nagent~ a")
	require.Equal(t, protocol.StateWorking, a.Detect(nil, time.Minute))
	kind, _ := a.Waiting()
	require.Equal(t, protocol.WaitSubagent, kind)
	replay(t, path, "agent- a")
	require.Equal(t, protocol.StateWorking, a.Detect(nil, 0), "the main agent should wake")
	*now = now.Add(wakeGrace)
	require.Equal(t, protocol.StateNeedsInput, a.Detect(nil, 0))
	require.Equal(t, protocol.WaitReview, a.WaitReason())
	kind, _ = a.Waiting()
	require.Empty(t, kind)
	// The main agent wakes after all: working, then its Stop.
	replay(t, path, "working\nwait-")
	require.Equal(t, protocol.StateWorking, a.Detect(nil, 0))
	require.Empty(t, a.WaitReason())
}

// A Stop right after a foreground Task wait parks from the wait's start.
func TestClaudeHooks_ParkedSinceTaskWait(t *testing.T) {
	a, path, now := waitingAdapter(t)
	replay(t, path, "working\nwait+ subagent", "agent+ a")
	_, _ = a.Waiting() // read the lines now
	start := *now
	*now = now.Add(time.Minute)
	replay(t, path, "needs_input\nturn-done")
	kind, since := a.Waiting()
	require.Equal(t, protocol.WaitSubagent, kind)
	require.Equal(t, start, since)
}

// A prompt line ends a parked wait on its own.
func TestClaudeHooks_PromptUnparks(t *testing.T) {
	a, path, _ := waitingAdapter(t)
	replay(t, path, "agent+ a", "needs_input\nturn-done", "working", "prompt")
	kind, _ := a.Waiting()
	require.Empty(t, kind)
}

// escTicks ticks Detect once a second from the Esc for d, with a still
// screen, and returns the first time (since the Esc) Detect said READY, or
// -1.
func escTicks(r *readyEdges, now *time.Time, d time.Duration) time.Duration {
	first := time.Duration(-1)
	for el := time.Duration(0); el <= d; el += time.Second {
		if r.tick(el) == protocol.StateNeedsInput && first < 0 {
			first = el
		}
		*now = now.Add(time.Second)
	}
	return first
}

// Esc during a Bash call: Claude fires no Stop and no PostToolUse, so the
// shell wait stays open. The Esc keystroke ends the turn within escBound,
// with exactly one READY.
func TestClaudeHooks_EscDuringBashWaitReadyFast(t *testing.T) {
	a, path, now := waitingAdapter(t)
	replay(t, path, "working\nwait-\nprompt", "working\nwait+ shell")
	r := &readyEdges{a: a, last: protocol.StateWorking}
	r.tick(0)
	a.UserInterrupt()
	first := escTicks(r, now, time.Minute)
	require.Equal(t, escBound, first, "READY once escBound passes")
	require.Equal(t, 1, r.edges, "READY exactly once")
	kind, _ := a.Waiting()
	require.Empty(t, kind)
}

// Esc during a foreground Task call: the subagent is killed, often with no
// SubagentStop, and the Task call never returns. READY within escBound.
func TestClaudeHooks_EscDuringTaskWaitReadyFast(t *testing.T) {
	a, path, now := waitingAdapter(t)
	replay(t, path, "working\nwait-\nprompt", "working\nwait+ subagent", "agent+ sub-1", "working", "working\nagent~ sub-1")
	r := &readyEdges{a: a, last: protocol.StateWorking}
	r.tick(0)
	*now = now.Add(time.Second)
	a.UserInterrupt()
	first := escTicks(r, now, time.Minute)
	require.Equal(t, escBound, first)
	require.Equal(t, 1, r.edges)
	require.Zero(t, a.Subagents(), "the killed foreground subagent no longer counts")
	kind, _ := a.Waiting()
	require.Empty(t, kind)
}

// Esc while a background subagent runs (launched earlier in the turn): the
// main turn is interrupted, the subagent runs on. No READY until it ends
// and the main agent does not wake; then READY exactly once.
func TestClaudeHooks_EscWithBackgroundSubagentWaits(t *testing.T) {
	a, path, now := waitingAdapter(t)
	replay(t, path,
		"working\nwait-\nprompt",
		"working\nwait+ subagent", "agent+ bg-1", "working\nwait-", // background Task returns at once
		"working\nwait+ shell",
	)
	r := &readyEdges{a: a, last: protocol.StateWorking}
	r.tick(0)
	a.UserInterrupt()
	require.Equal(t, time.Duration(-1), escTicks(r, now, 5*time.Minute), "no READY while the background subagent runs")
	kind, _ := a.Waiting()
	require.Equal(t, protocol.WaitSubagent, kind)
	require.Equal(t, 1, a.Subagents())
	// It keeps working, writing its own lines, then ends.
	replay(t, path, "working", "working\nagent~ bg-1")
	require.Equal(t, protocol.StateWorking, r.tick(time.Minute))
	replay(t, path, "agent- bg-1")
	first := escTicks(r, now, time.Minute)
	require.Equal(t, wakeGrace, first, "READY after the wake grace")
	require.Equal(t, 1, r.edges)
}

// An Esc that did not end the turn (it closed a menu): the main thread's
// next hook line cancels it, and the turn goes on.
func TestClaudeHooks_EscCancelledByMainThreadLine(t *testing.T) {
	a, path, now := waitingAdapter(t)
	replay(t, path, "working\nwait-\nprompt", "working\nwait+ shell")
	a.UserInterrupt()
	*now = now.Add(time.Second)
	replay(t, path, "working\nwait-") // the command returned
	*now = now.Add(escBound)
	require.Equal(t, protocol.StateWorking, a.Detect(nil, escBound))
}

// A foreground subagent that shows life after the Esc was not killed: no
// READY while it runs.
func TestClaudeHooks_EscForegroundSubagentAliveStaysWorking(t *testing.T) {
	a, path, now := waitingAdapter(t)
	replay(t, path, "working\nwait-\nprompt", "working\nwait+ subagent", "agent+ sub-1")
	a.UserInterrupt()
	*now = now.Add(time.Second)
	replay(t, path, "working\nagent~ sub-1")
	*now = now.Add(escBound)
	require.Equal(t, protocol.StateWorking, a.Detect(nil, escBound))
	kind, _ := a.Waiting()
	require.Equal(t, protocol.WaitSubagent, kind)
}

// An Esc at a finished turn changes nothing; the next prompt clears it.
func TestClaudeHooks_EscAtReadyThenPrompt(t *testing.T) {
	a, path, now := waitingAdapter(t)
	replay(t, path, "working\nwait-\nprompt", "needs_input\nturn-done")
	a.UserInterrupt()
	require.Equal(t, protocol.StateNeedsInput, a.Detect(nil, 0))
	replay(t, path, "working\nwait-\nprompt", "working\nwait+ shell")
	*now = now.Add(time.Minute)
	require.Equal(t, protocol.StateWorking, a.Detect(nil, time.Minute), "the old Esc does not end the new turn")
}

// Esc during a fan-out of two Task calls, one already returned: both
// killed subagents stop counting.
func TestClaudeHooks_EscDuringTaskFanOut(t *testing.T) {
	a, path, now := waitingAdapter(t)
	replay(t, path, "working\nwait-\nprompt",
		"working\nwait+ subagent", "agent+ a",
		"working\nwait+ subagent", "agent+ b",
		"working\nwait-", // one Task call returned, both subagents run on
	)
	require.Equal(t, 2, a.Subagents()) // read the lines before the Esc
	*now = now.Add(time.Second)
	a.UserInterrupt()
	r := &readyEdges{a: a, last: protocol.StateWorking}
	require.Equal(t, escBound, escTicks(r, now, time.Minute))
	require.Equal(t, 1, r.edges)
	require.Zero(t, a.Subagents())
}

// Esc during a Task call with a Bash call in parallel (the shell wait took
// the slot): the killed subagent still stops counting.
func TestClaudeHooks_EscTaskAndBashParallel(t *testing.T) {
	a, path, now := waitingAdapter(t)
	replay(t, path, "working\nwait-\nprompt", "working\nwait+ subagent", "agent+ a", "working\nwait+ shell")
	require.Equal(t, 1, a.Subagents()) // read the lines before the Esc
	*now = now.Add(time.Second)
	a.UserInterrupt()
	r := &readyEdges{a: a, last: protocol.StateWorking}
	require.Equal(t, escBound, escTicks(r, now, time.Minute))
	require.Zero(t, a.Subagents())
}

// An Esc while parked on a background subagent (the main turn ended)
// interrupts nothing, even after the subagent's own working word.
func TestClaudeHooks_EscWhileParkedIgnored(t *testing.T) {
	a, path, now := waitingAdapter(t)
	replay(t, path, "agent+ bg", "needs_input\nturn-done", "working\nagent~ bg")
	a.UserInterrupt()
	*now = now.Add(time.Minute)
	require.Equal(t, protocol.StateWorking, a.Detect(nil, time.Minute))
	require.Equal(t, 1, a.Subagents())
	require.True(t, a.escAt.IsZero())
}

// A truncated hook file drops a pending Esc.
func TestClaudeHooks_TruncateDropsEsc(t *testing.T) {
	a, path, now := waitingAdapter(t)
	replay(t, path, "working\nwait-\nprompt", "working\nwait+ shell")
	a.UserInterrupt()
	truncate(t, path)
	replay(t, path, "working")
	*now = now.Add(escBound)
	require.Equal(t, protocol.StateWorking, a.Detect(nil, time.Second))
}
