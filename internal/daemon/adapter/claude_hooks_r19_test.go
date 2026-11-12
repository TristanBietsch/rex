package adapter

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// R19 replay tests: hook lines as Claude Code writes them (the current hook
// commands), with an injected clock. READY (needs_input) must only show
// when the user is needed. See docs/plans/r2/R19-ready-edge-cases.md.

// Lines each hook writes, by event (main thread unless named sub).
const (
	hPrompt     = "working\nprompt"           // UserPromptSubmit (also a background report)
	hLegacyWake = "working\nwait-\nprompt"    // R17 UserPromptSubmit
	hTool       = "working\nwait-"            // main PreToolUse/PostToolUse of a plain tool
	hShell      = "working\nwait+ shell"      // main PreToolUse Bash
	hTask       = "working\nwait+ subagent"   // main PreToolUse Agent/Task
	hLaunch     = "working\nwait- launch"     // main PostToolUse Agent, async_launched
	hStop       = "needs_input\nturn-done"    // Stop
	hSubPre     = "working"                   // subagent PreToolUse
	hSubNest    = "working\nnest+"            // subagent PreToolUse Agent/Task
	hCompacted  = "compacted\nsid " + testSID // SessionStart compact
)

func hSubPost(id string) string { return "working\nagent~ " + id }
func hNotify(ty string) string  { return "needs_input\nnotify " + ty }

// tickN ticks Detect n times one second apart with a still screen of idle.
func tickN(r *readyEdges, now *time.Time, n int, idle time.Duration) protocol.State {
	var st protocol.State
	for range n {
		st = r.tick(idle)
		*now = now.Add(time.Second)
	}
	return st
}

// The user's report: a background subagent's update wakes the main agent.
// With R17 (and R19) no READY until the last report is handled.
func TestClaudeHooks_R19ReportedSymptom(t *testing.T) {
	a, path, now := waitingAdapter(t)
	r := &readyEdges{a: a, last: protocol.StateWorking}
	step := func(lines ...string) {
		replay(t, path, lines...)
		tickN(r, now, 3, 20*time.Second)
	}
	step(hPrompt)
	step(hTask, "agent+ a", hLaunch)
	step(hTask, "agent+ b", hLaunch)
	step(hStop) // both run in the background
	step(hSubPre, hSubPost("a"), "agent- a")
	step(hPrompt) // a's report
	step(hTool, hTool)
	step(hStop) // b still runs
	step(hSubPost("b"), "agent- b")
	step(hPrompt) // b's report
	step(hTool)
	require.Zero(t, r.edges, "no READY while a subagent runs or reports")
	step(hStop)
	require.Equal(t, protocol.StateNeedsInput, r.last)
	require.Equal(t, 1, r.edges)
	require.Equal(t, protocol.WaitReview, a.WaitReason())
}

// A background subagent ends while the main turn is still busy; its report
// only reaches the main agent after the turn's Stop.
func TestClaudeHooks_R19BusyEndThenStop(t *testing.T) {
	for _, tc := range []struct {
		name string
		busy []string
	}{
		{"busy", []string{hShell, "agent- a", hTool}},
		{"right at stop", []string{"agent- a"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, path, now := waitingAdapter(t)
			r := &readyEdges{a: a, last: protocol.StateWorking}
			replay(t, path, hPrompt, hTask, "agent+ a", hLaunch)
			replay(t, path, tc.busy...)
			replay(t, path, hStop)
			require.Equal(t, protocol.StateWorking, tickN(r, now, 3, time.Minute), "the report is owed")
			kind, _ := a.Waiting()
			require.Equal(t, protocol.WaitSubagent, kind)
			replay(t, path, hPrompt, hTool, hStop)
			require.Equal(t, protocol.StateNeedsInput, tickN(r, now, 1, 0))
			require.Equal(t, 1, r.edges)
		})
	}
}

// Two ends, one batched report: READY after wakeGrace, never stuck.
func TestClaudeHooks_R19OwedReportBounded(t *testing.T) {
	a, path, now := waitingAdapter(t)
	r := &readyEdges{a: a, last: protocol.StateWorking}
	replay(t, path, hPrompt, hTask, "agent+ a", hLaunch, hTask, "agent+ b", hLaunch, hShell, "agent- a", "agent- b", hTool, hStop)
	require.Equal(t, protocol.StateWorking, tickN(r, now, 2, time.Minute))
	replay(t, path, hPrompt, hStop)
	require.Equal(t, protocol.StateWorking, tickN(r, now, 1, 0), "one report may still come")
	require.Equal(t, protocol.StateNeedsInput, tickN(r, now, int(wakeGrace/time.Second)+1, time.Minute))
	require.Equal(t, 1, r.edges)
	require.Equal(t, protocol.WaitReview, a.WaitReason())
}

// Foreground subagent results come back through the Task call: no owed
// report, READY right at the Stop. Nested subagents report to their parent.
func TestClaudeHooks_R19ForegroundAndNestedOweNothing(t *testing.T) {
	a, path, _ := waitingAdapter(t)
	replay(t, path, hPrompt, hTask, "agent+ p", hSubNest, "agent+ n", hSubPost("n"), "agent- n", hSubPost("p"), "agent- p", hTool, hStop)
	require.Equal(t, protocol.StateNeedsInput, a.Detect(nil, 0))
}

// A nested subagent ending while the main turn is busy owes no report.
func TestClaudeHooks_R19NestedBusyOwesNothing(t *testing.T) {
	a, path, _ := waitingAdapter(t)
	replay(t, path, hPrompt, hTask, "agent+ p", hLaunch, hShell, hSubNest, "agent+ n", "agent- n", hTool)
	require.Equal(t, 1, a.Subagents())
	replay(t, path, "agent- p", hPrompt, hStop) // p's report delivered
	require.Equal(t, protocol.StateNeedsInput, a.Detect(nil, 0))
}

// Notifications that need nobody change nothing; a question is READY at
// once and keeps the open tool wait for after the answer.
func TestClaudeHooks_R19InfoNotificationNoChange(t *testing.T) {
	for _, ty := range []string{"auth_success", "auth_storage_failure", "elicitation_complete", "elicitation_response",
		"agent_completed", "quota_auto_resume_fired", "plugin_notification", "some_future_type"} {
		t.Run(ty, func(t *testing.T) {
			a, path, now := waitingAdapter(t)
			replay(t, path, hPrompt, hShell)
			_, start := a.Waiting() // read the lines now
			replay(t, path, hNotify(ty))
			*now = now.Add(time.Second)
			require.Equal(t, protocol.StateWorking, a.Detect(nil, time.Second))
			kind, since := a.Waiting()
			require.Equal(t, protocol.WaitShell, kind)
			require.Equal(t, start, since)
			require.Empty(t, a.WaitReason())
		})
	}
}

func TestClaudeHooks_R19QuestionKeepsWait(t *testing.T) {
	for _, ty := range []string{"permission_prompt", "elicitation_dialog", "elicitation_url_dialog", "agent_needs_input", "quota_auto_resume_stale", ""} {
		t.Run(ty, func(t *testing.T) {
			a, path, _ := waitingAdapter(t)
			replay(t, path, hPrompt, hTask, "agent+ a", hSubPre, hNotify(ty))
			require.Equal(t, protocol.StateNeedsInput, a.Detect(nil, 0))
			require.Equal(t, protocol.WaitInput, a.WaitReason())
			replay(t, path, hSubPost("a")) // answered: the subagent goes on
			require.Equal(t, protocol.StateWorking, a.Detect(nil, time.Minute))
			kind, _ := a.Waiting()
			require.Equal(t, protocol.WaitSubagent, kind)
		})
	}
}

// idle_prompt and quota_auto_resume_disabled end the turn, not a question.
func TestClaudeHooks_R19TurnEndNotifications(t *testing.T) {
	for _, ty := range []string{"idle_prompt", "quota_auto_resume_disabled"} {
		t.Run(ty, func(t *testing.T) {
			a, path, _ := waitingAdapter(t)
			replay(t, path, hPrompt, hTool, hNotify(ty))
			require.Equal(t, protocol.StateNeedsInput, a.Detect(nil, 0))
			require.NotEqual(t, protocol.WaitInput, a.WaitReason())
		})
	}
}

// An auto-compaction mid-turn is not READY.
func TestClaudeHooks_R19CompactionMidTurn(t *testing.T) {
	a, path, _ := waitingAdapter(t)
	replay(t, path, hPrompt, hTask, "agent+ a", hLaunch, hTool, hCompacted)
	require.Equal(t, protocol.StateWorking, a.Detect(nil, 5*time.Second))
	require.Equal(t, testSID, a.ResumeID())
	require.Equal(t, 1, a.Subagents())
}

// A lost SubagentStop: the Stop's background_tasks count drops it once it
// is quiet for reconcileQuiet, and the session is READY after the wake
// grace, not after 15 minutes.
func TestClaudeHooks_R19BgAgentsReconcile(t *testing.T) {
	a, path, now := waitingAdapter(t)
	r := &readyEdges{a: a, last: protocol.StateWorking}
	replay(t, path, hPrompt, hTask, "agent+ lost", hLaunch, hTask, "agent+ live", hLaunch)
	require.Equal(t, 2, a.Subagents()) // read the lines now
	*now = now.Add(time.Second)
	replay(t, path, hSubPost("live"), hStop+"\nbg-agents 1")
	require.Equal(t, 2, a.Subagents(), "not quiet long enough yet")
	*now = now.Add(reconcileQuiet - 2*time.Second)
	replay(t, path, hSubPost("live"))
	require.Equal(t, 2, a.Subagents())
	*now = now.Add(2 * time.Second)
	require.Equal(t, 1, a.Subagents(), "the quiet one above the count goes")
	replay(t, path, hSubPost("live"), "agent- live")
	require.Equal(t, protocol.StateWorking, tickN(r, now, 3, time.Minute))
	require.Equal(t, protocol.StateNeedsInput, tickN(r, now, int(wakeGrace/time.Second), time.Minute))
	require.Equal(t, 1, r.edges)

	// A count above the known set changes nothing; nor does a busy
	// subagent above a miscounted Stop.
	b, pathB, _ := waitingAdapter(t)
	replay(t, pathB, hPrompt, hTask, "agent+ a", hLaunch, hStop+"\nbg-agents 3")
	require.Equal(t, 1, b.Subagents())
	require.Equal(t, protocol.StateWorking, b.Detect(nil, time.Minute))
	c, pathC, nowC := waitingAdapter(t)
	replay(t, pathC, hPrompt, hTask, "agent+ busy", hLaunch, hStop+"\nbg-agents 0")
	for range 5 {
		*nowC = nowC.Add(30 * time.Second)
		replay(t, pathC, hSubPost("busy"))
		require.Equal(t, protocol.StateWorking, c.Detect(nil, time.Minute))
	}
}

// A background Agent call returns at once; an Esc after it interrupts the
// main turn but not the background subagent.
func TestClaudeHooks_R19LaunchThenEscKeepsSubagent(t *testing.T) {
	a, path, now := waitingAdapter(t)
	replay(t, path, hPrompt, hTask, "agent+ bg", hLaunch, hShell)
	require.Equal(t, 1, a.Subagents())
	*now = now.Add(time.Second)
	a.UserInterrupt()
	r := &readyEdges{a: a, last: protocol.StateWorking}
	require.Equal(t, protocol.StateWorking, tickN(r, now, 10, time.Minute))
	require.Equal(t, 1, a.Subagents())
	kind, _ := a.Waiting()
	require.Equal(t, protocol.WaitSubagent, kind)
}

// Parallel launches: one foreground Task still running when a background
// launch returns. No READY while either runs.
func TestClaudeHooks_R19LaunchKeepsCount(t *testing.T) {
	a, path, now := waitingAdapter(t)
	replay(t, path, hPrompt, hTask, "agent+ fg", hTask, "agent+ bg", hLaunch)
	*now = now.Add(time.Minute)
	require.Equal(t, protocol.StateWorking, a.Detect(nil, time.Minute))
	require.Equal(t, 2, a.Subagents())
}

// A late sign of life for an ended subagent does not bring it back; a new
// start (resume, same id) does.
func TestClaudeHooks_R19EndedStaysEnded(t *testing.T) {
	a, path, _ := waitingAdapter(t)
	replay(t, path, "agent+ a", "agent- a", hSubPost("a"))
	require.Zero(t, a.Subagents())
	replay(t, path, "agent+ a")
	require.Equal(t, 1, a.Subagents())
	replay(t, path, "agent- a")
	require.Zero(t, a.Subagents())
}

// StopFailure (API error) ends the turn like Stop.
func TestClaudeHooks_R19StopFailureHook(t *testing.T) {
	cmds := hookCommands(t)
	require.Contains(t, cmds, "StopFailure")
	a, path, _ := waitingAdapter(t)
	replay(t, path, hPrompt, hShell)
	runHook(t, cmds["StopFailure"], path, `{"session_id":"s","hook_event_name":"StopFailure","error":"rate_limit"}`)
	require.Equal(t, protocol.StateNeedsInput, a.Detect(nil, 0))
	require.Equal(t, protocol.WaitReview, a.WaitReason())
}

// The hook commands write the R19 lines.
func TestClaudeHooks_R19HookLines(t *testing.T) {
	cmds := hookCommands(t)
	for _, tc := range []struct {
		name, event, stdin, want string
	}{
		{"prompt", "UserPromptSubmit", `{"prompt":"hi"}`, hPrompt + "\n"},
		{"compact", "SessionStart", `{"source":"compact","session_id":"` + testSID + `"}`, hCompacted + "\n"},
		{"startup", "SessionStart", `{"source":"startup","session_id":"` + testSID + `"}`, "agents-reset\nneeds_input\nsid " + testSID + "\n"},
		{"stop no registry", "Stop", `{"stop_hook_active":false,"last_assistant_message":"say \"background_tasks\":[{\"type\":\"subagent\"}]"}`, hStop + "\n"},
		{"stop bg", "Stop", `{"stop_hook_active":false,"background_tasks":[{"id":"t1","type":"subagent","status":"running"},{"id":"t2","type": "shell","status":"running"},{"id":"t3", "type" : "subagent"}],"session_crons":[]}`, hStop + "\nbg-agents 2\n"},
		{"stop empty", "Stop", `{"background_tasks":[],"session_crons":[]}`, hStop + "\nbg-agents 0\n"},
		{"launch", "PostToolUse", `{"tool_name":"Agent","tool_response":{"status":"async_launched","agentId":"x"}}`, hLaunch + "\n"},
		{"fg result", "PostToolUse", `{"tool_name":"Agent","tool_response":{"status":"completed"},"tool_input":{"prompt":"say \"async_launched\""}}`, hTool + "\n"},
		{"sub agent call", "PreToolUse", `{"agent_id":"p1","agent_type":"x","tool_name":"Agent"}`, hSubNest + "\n"},
		{"sub read", "PreToolUse", `{"agent_id":"p1","agent_type":"x","tool_name":"Read"}`, hSubPre + "\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := t.TempDir() + "/hooks.log"
			runHook(t, cmds[tc.event], path, tc.stdin)
			got, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, tc.want, string(got), strings.ReplaceAll(cmds[tc.event], "\n", " "))
		})
	}
}

// R17-format files (sessions spawned before an upgrade) keep working: the
// old UserPromptSubmit lines still wake and count.
func TestClaudeHooks_R19LegacyWake(t *testing.T) {
	a, path, _ := waitingAdapter(t)
	replay(t, path, hLegacyWake, hTask, "agent+ a", "working\nwait-", hStop, "agent- a", hLegacyWake, hTool, hStop)
	require.Equal(t, protocol.StateNeedsInput, a.Detect(nil, 0))
	require.Equal(t, 2, a.Prompts())
}

// A permission prompt from one subagent stays READY while a sibling
// subagent works; the asking subagent's next tool call answers it.
func TestClaudeHooks_R19SubagentQuestionWaitsForAsker(t *testing.T) {
	a, path, _ := waitingAdapter(t)
	replay(t, path, hPrompt, hTask, "agent+ asker", hLaunch, hTask, "agent+ sib", hLaunch, hStop)
	replay(t, path, hNotify("permission_prompt")+"\nasker asker")
	require.Equal(t, protocol.StateNeedsInput, a.Detect(nil, 0))
	replay(t, path, hSubPre, hSubPost("sib"), hSubPre)
	require.Equal(t, protocol.StateNeedsInput, a.Detect(nil, 0), "a sibling's work is no answer")
	require.Equal(t, protocol.WaitInput, a.WaitReason())
	replay(t, path, hSubPost("asker"))
	require.Equal(t, protocol.StateWorking, a.Detect(nil, 0))
	kind, _ := a.Waiting()
	require.Equal(t, protocol.WaitSubagent, kind)

	// An asker that ends (or goes quiet) ends its question.
	replay(t, path, hNotify("permission_prompt")+"\nasker sib", "agent- sib")
	require.Equal(t, protocol.StateWorking, a.Detect(nil, 0))
}

// The Notification hook names the asking subagent.
func TestClaudeHooks_R19NotificationAskerLine(t *testing.T) {
	cmds := hookCommands(t)
	path := t.TempDir() + "/hooks.log"
	runHook(t, cmds["Notification"], path, `{"notification_type":"permission_prompt","agent_id":"sub-1","agent_type":"x"}`)
	runHook(t, cmds["Notification"], path, `{"notification_type":"permission_prompt","message":"say \"agent_id\": \"x\""}`)
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "needs_input\nnotify permission_prompt\nasker sub-1\nneeds_input\nnotify permission_prompt\n", string(got))
}

// A nest+ whose Agent call never started a subagent expires: the next
// background subagent still owes its report.
func TestClaudeHooks_R19NestExpires(t *testing.T) {
	a, path, now := waitingAdapter(t)
	// A foreground subagent's own Agent call is denied: no nested start.
	replay(t, path, hPrompt, hTask, "agent+ p", hSubNest, hSubPost("p"), "agent- p", hTool)
	require.Zero(t, a.Subagents()) // read the lines now
	*now = now.Add(nestExpire)
	replay(t, path, hTask, "agent+ b", hLaunch, hShell, "agent- b", hTool, hStop)
	require.Equal(t, protocol.StateWorking, a.Detect(nil, 0), "b's report is owed")
	replay(t, path, hPrompt, hStop)
	require.Equal(t, protocol.StateNeedsInput, a.Detect(nil, 0))
}

// Esc with a report owed: the wake grace counts from the Esc, so READY
// comes at most wakeGrace after it.
func TestClaudeHooks_R19EscWithOwedReport(t *testing.T) {
	a, path, now := waitingAdapter(t)
	replay(t, path, hPrompt, hTask, "agent+ b", hLaunch, hShell, "agent- b")
	require.Zero(t, a.Subagents()) // read the lines now
	a.UserInterrupt()
	r := &readyEdges{a: a, last: protocol.StateWorking}
	require.Equal(t, wakeGrace, escTicks(r, now, time.Minute))
	require.Equal(t, 1, r.edges)
}

// An informational notification does not cancel a pending Esc.
func TestClaudeHooks_R19InfoNotificationKeepsEsc(t *testing.T) {
	a, path, now := waitingAdapter(t)
	replay(t, path, hPrompt, hShell)
	a.UserInterrupt()
	replay(t, path, hNotify("plugin_notification"))
	r := &readyEdges{a: a, last: protocol.StateWorking}
	require.Equal(t, escBound, escTicks(r, now, time.Minute))
}

// A truncated file drops a pending question.
func TestClaudeHooks_R19TruncateDropsQuestion(t *testing.T) {
	a, path, _ := waitingAdapter(t)
	replay(t, path, hPrompt, hShell, hNotify("permission_prompt"))
	require.Equal(t, protocol.StateNeedsInput, a.Detect(nil, 0))
	truncate(t, path)
	replay(t, path, hSubPre)
	require.Equal(t, protocol.StateWorking, a.Detect(nil, 0))
	require.Empty(t, a.WaitReason())
}

// A pre-R17 file (UserPromptSubmit wrote a lone working word): Stop is
// READY, the next prompt is WORKING.
func TestClaudeHooks_R19PreR17File(t *testing.T) {
	a, path, _ := waitingAdapter(t)
	replay(t, path, "working", "working\nwait-", "needs_input\nturn-done")
	require.Equal(t, protocol.StateNeedsInput, a.Detect(nil, 0))
	replay(t, path, "working")
	require.Equal(t, protocol.StateWorking, a.Detect(nil, 0))
}
