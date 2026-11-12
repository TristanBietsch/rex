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
