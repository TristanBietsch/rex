package adapter

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// HookFileEnv names the env var holding the per-session hook state file. The
// supervisor sets it on the child; Claude Code passes env through to hooks.
const HookFileEnv = "REX_HOOK_FILE"

// hookStaleWorking is how long a hook-reported "working" survives without any
// visible PTY output when no subagent runs and no tool wait is open. A turn
// interrupted with Esc fires no Stop hook, so silence this long ends it.
// There is no cheap screen check to confirm it (Claude's input box shows
// while it works too), so the bound is generous. With a tool wait open the
// bound is subagentQuiet; while a subagent runs the fallback never fires.
// An interrupt key the daemon forwarded (UserInterrupt) ends the turn much
// sooner, after escBound.
const hookStaleWorking = 30 * time.Second

// escBound is how long after an interrupt key (Esc, Ctrl+C) written to the
// session the turn counts as interrupted, when no main-thread hook line came
// since. Claude fires no Stop, PostToolUse or PostToolUseFailure for an
// interrupted turn or a cancelled tool, so the key is the only signal.
const escBound = 5 * time.Second

// wakeGrace is how long a parked session (main turn ended, background
// subagents running) stays working after its last subagent ends: Claude
// wakes the main agent with the result, which writes a working word, and
// only its next Stop is READY. If no hook comes in time, it is READY.
const wakeGrace = 10 * time.Second

// claudeHookStates maps Claude Code hook events to the state each implies.
// SessionStart, PreToolUse and PostToolUse also write extra lines: subagent,
// session id and tool-wait lines (see claudeHookCommand).
var claudeHookStates = []struct {
	event   string
	matcher bool // tool events take a matcher
	state   protocol.State
}{
	{"SessionStart", false, protocol.StateNeedsInput},
	{"UserPromptSubmit", false, protocol.StateWorking},
	{"PreToolUse", true, protocol.StateWorking},
	{"PostToolUse", true, protocol.StateWorking},
	{"PostToolUseFailure", true, protocol.StateWorking},
	{"Notification", false, protocol.StateNeedsInput}, // permission / idle prompt; writes a notify line
	{"Stop", false, protocol.StateNeedsInput},         // turn finished, waiting on user; writes turn-done
	{"StopFailure", false, protocol.StateNeedsInput},  // turn ended by an API error: same lines as Stop
}

// Subagent lines in the hook file, next to the state words:
//
//	agent+ <agent_id>  SubagentStart
//	agent- <agent_id>  SubagentStop
//	agent~ <agent_id>  PostToolUse of a subagent's tool call (still running)
//	agents-reset       SessionStart of a new or cleared conversation
//	compacted          SessionStart after a compaction (no state change)
//	sid <session_id>   SessionStart: the conversation id (resume id)
//	turn-done          Stop, StopFailure: the agent finished a turn
//	                   (wait_reason review)
//	bg-agents <n>      Stop: running background subagents in the Stop
//	                   input's background_tasks (only when present)
//	notify <type>      Notification: notification_type, "" if absent
//	asker <agent_id>   Notification from a subagent: which one asks
//	prompt             UserPromptSubmit: a prompt reached the main agent (a
//	                   new turn; also a background subagent's report)
//	wait+ <kind>       PreToolUse of a main-thread shell, subagent or web
//	                   tool (kind: shell, subagent, web)
//	wait-              PreToolUse of another main-thread tool, PostToolUse
//	                   of a main-thread tool call
//	wait- launch       PostToolUse of a main-thread Agent/Task call that
//	                   started a background subagent (async_launched)
//	nest+              PreToolUse of a subagent's Agent/Task call: the next
//	                   subagent started is nested (it reports to its parent)
//
// The wait reason lines follow the needs_input word of their own event, so
// the word itself never clears the reason.
const (
	hookAgentStart  = "agent+"
	hookAgentStop   = "agent-"
	hookAgentSeen   = "agent~"
	hookAgentsReset = "agents-reset"
	hookSessionID   = "sid"
	hookTurnDone    = "turn-done"
	hookNotify      = "notify"
	hookWaitStart   = "wait+"
	hookWaitEnd     = "wait-"
	hookPrompt      = "prompt"
	hookCompacted   = "compacted"
	hookBgAgents    = "bg-agents"
	hookNest        = "nest+"
	hookAsker       = "asker"
	waitLaunch      = "launch" // wait- argument
)

// Notification classes (notifyClass).
const (
	notifyQuestion = "question" // waits on the user: READY at once
	notifyTurnEnd  = "turn-end" // the turn is over: like a Stop without review
	notifyInfo     = "info"     // needs nobody: no state change
)

// notifyClass sorts a Notification's notification_type. Only the types
// that wait on the user are questions; idle_prompt (about 60 s after a
// turn) and quota_auto_resume_disabled (a usage-limit wait given up) end
// the turn. Every other type (auth_success, elicitation_complete,
// elicitation_response, agent_completed, quota_auto_resume_fired,
// plugin_notification, any newer type) needs nobody. An absent type (old
// Claude versions) was a permission or idle message: a question.
func notifyClass(ty string) string {
	switch ty {
	case "", "permission_prompt", "elicitation_dialog", "elicitation_url_dialog", "agent_needs_input", "quota_auto_resume_stale":
		return notifyQuestion
	case "idle_prompt", "quota_auto_resume_disabled":
		return notifyTurnEnd
	}
	return notifyInfo
}

// subagentQuiet is how long a subagent may go without a start or a tool
// call before it stops counting. It bounds a count stuck by a lost
// SubagentStop; a quiet subagent that calls a tool again is counted again.
const subagentQuiet = 15 * time.Minute

// hookJSONField is a shell pipeline printing a top-level string field of the
// hook's JSON (held in $in). Splitting on commas puts each key at the start
// of a line, and only a line that starts with the key counts, so a field of
// a nested object ("x":{"agent_id":…}) or one quoted inside a string value
// (\"agent_id\") never matches; the first match is the top-level field.
// Plain sed and tr, no jq: hooks also run in Git Bash on Windows.
func hookJSONField(name string) string {
	return `printf '%s' "$in" | tr ',' '\n' | sed -n 's/^[{[:space:]]*"` + name + `"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1`
}

// hookSafeID cuts an id to characters that cannot break a hook line.
const hookSafeID = ` | tr -cd 'A-Za-z0-9_.:-'`

// hookAppend appends the output of body to $REX_HOOK_FILE and always exits
// 0: a hook must never block Claude. stderr is silenced first, so a failed
// open is quiet too.
func hookAppend(body string) string {
	return `{ ` + body + `; } 2>/dev/null >>"$` + HookFileEnv + `"; exit 0`
}

// subagentHookCommand writes "<word> <agent_id>" for a SubagentStart or
// SubagentStop event. An event without an agent_type is one of Claude's
// internal helpers (prompt suggestions, /btw), not a subagent the user
// started, and is skipped.
func subagentHookCommand(word string) string {
	return `in=$(cat); id=$(` + hookJSONField("agent_id") + hookSafeID + `); ty=$(` + hookJSONField("agent_type") + `); ` +
		hookAppend(`if [ -n "$id" ] && [ -n "$ty" ]; then printf '`+word+` %s\n' "$id"; fi`)
}

// hookAgentTool is a shell case pattern for the tools that start a subagent.
const hookAgentTool = `Task|Agent`

// hookToolName is a shell command substitution printing the tool_name.
var hookToolName = `$(` + hookJSONField("tool_name") + `)`

// claudeHookCommand is the command for one state event. SessionStart also
// resets the subagent set, except after a compaction (subagents may still
// be running, and the turn goes on: no state word), and reports the
// conversation's session_id (a `sid` line in the same printf, so it is
// atomic with the state word). PostToolUse also marks a subagent's tool
// call (the JSON has an agent_id) as a sign of life. Stop and StopFailure
// count the running background subagents in background_tasks (a quoted
// "type":"subagent" outside a string; inside one the quotes are escaped).
func claudeHookCommand(event string, state protocol.State) string {
	st := string(state)
	switch event {
	case "SessionStart":
		return `in=$(cat); src=$(` + hookJSONField("source") + `); sid=$(` + hookJSONField("session_id") + hookSafeID + `); ` +
			hookAppend(`if [ "$src" = compact ]; then printf '`+hookCompacted+`\n`+hookSessionID+` %s\n' "$sid"; else printf '`+hookAgentsReset+`\n`+st+`\n`+hookSessionID+` %s\n' "$sid"; fi`)
	case "PostToolUse", "PostToolUseFailure":
		// A main-thread Agent call that returned at once (async_launched)
		// started a background subagent.
		return `in=$(cat); id=; case "$in" in *'"agent_id"'*) id=$(` + hookJSONField("agent_id") + hookSafeID + `);; esac; l=; ` +
			`if [ -z "$id" ]; then case "$in" in *'"async_launched"'*) case "` + hookToolName + `" in ` + hookAgentTool + `) l=' ` + waitLaunch + `';; esac;; esac; fi; ` +
			hookAppend(`if [ -n "$id" ]; then printf '`+st+`\n`+hookAgentSeen+` %s\n' "$id"; else printf '`+st+`\n`+hookWaitEnd+`%s\n' "$l"; fi`)
	case "Stop", "StopFailure":
		return `in=$(cat); n=; case "$in" in *'"background_tasks"'*) n=$(printf '%s' "$in" | grep -o '"type"[[:space:]]*:[[:space:]]*"subagent"' | wc -l | tr -cd '0-9');; esac; ` +
			hookAppend(`if [ -n "$n" ]; then printf '`+st+`\n`+hookTurnDone+`\n`+hookBgAgents+` %s\n' "$n"; else printf '`+st+`\n`+hookTurnDone+`\n'; fi`)
	case "Notification":
		// A question from a subagent names it (asker line), so only its
		// own next tool call counts as the answer.
		return `in=$(cat); ty=$(` + hookJSONField("notification_type") + hookSafeID + `); id=; case "$in" in *'"agent_id"'*) id=$(` + hookJSONField("agent_id") + hookSafeID + `);; esac; ` +
			hookAppend(`if [ -n "$id" ]; then printf '`+st+`\n`+hookNotify+` %s\n`+hookAsker+` %s\n' "$ty" "$id"; else printf '`+st+`\n`+hookNotify+` %s\n' "$ty"; fi`)
	case "PreToolUse":
		// Only a main-thread call (no agent_id) can mark the session as
		// waiting. The kind is picked by a shell case, so only the three
		// fixed words are ever written.
		// A subagent's own Agent call announces a nested subagent.
		return `in=$(cat); id=; case "$in" in *'"agent_id"'*) id=$(` + hookJSONField("agent_id") + hookSafeID + `);; esac; k=; ` +
			`case "` + hookToolName + `" in Bash) k=` + protocol.WaitShell +
			`;; ` + hookAgentTool + `) k=` + protocol.WaitSubagent + `;; WebFetch|WebSearch) k=` + protocol.WaitWeb + `;; esac; ` +
			hookAppend(`if [ -n "$id" ] && [ "$k" = `+protocol.WaitSubagent+` ]; then printf '`+st+`\n`+hookNest+`\n'; elif [ -n "$id" ]; then printf '`+st+`\n'; elif [ -n "$k" ]; then printf '`+st+`\n`+hookWaitStart+` %s\n' "$k"; else printf '`+st+`\n`+hookWaitEnd+`\n'; fi`)
	case "UserPromptSubmit":
		// A new turn: the prompt line also ends the last turn's tool wait.
		return hookAppend(`printf '` + st + `\n` + hookPrompt + `\n'`)
	default:
		return hookAppend(`printf '` + st + `\n'`)
	}
}

// ClaudeHookArgs returns argv that makes Claude Code append one state word per
// hook event to the file named by $REX_HOOK_FILE, plus the subagent lines.
// Each hook writes its lines with one printf, so lines from hooks running in
// parallel never interleave.
func ClaudeHookArgs() []string { return ClaudeSettingsArgs("") }

// ClaudeSettingsArgs is ClaudeHookArgs plus, when statusLine is not empty, a
// "statusLine" entry running that command (the usage tap, see package
// usage). The hooks are the same either way; "" adds no statusLine.
func ClaudeSettingsArgs(statusLine string) []string {
	type hookCmd struct {
		Type    string `json:"type"`
		Command string `json:"command"`
	}
	type matcherGroup struct {
		Matcher string    `json:"matcher,omitempty"`
		Hooks   []hookCmd `json:"hooks"`
	}
	hooks := map[string][]matcherGroup{}
	for _, h := range claudeHookStates {
		g := matcherGroup{Hooks: []hookCmd{{Type: "command", Command: claudeHookCommand(h.event, h.state)}}}
		if h.matcher {
			g.Matcher = "*"
		}
		hooks[h.event] = append(hooks[h.event], g)
	}
	hooks["SubagentStart"] = []matcherGroup{{Hooks: []hookCmd{{Type: "command", Command: subagentHookCommand(hookAgentStart)}}}}
	hooks["SubagentStop"] = []matcherGroup{{Hooks: []hookCmd{{Type: "command", Command: subagentHookCommand(hookAgentStop)}}}}
	cfg := map[string]any{"hooks": hooks}
	if statusLine != "" {
		cfg["statusLine"] = map[string]string{"type": "command", "command": statusLine}
	}
	b, _ := json.Marshal(cfg)
	return []string{"--settings", string(b)}
}

// SubagentCounter is implemented by adapters that know how many subagents
// the agent is running.
type SubagentCounter interface {
	Subagents() int
}

// ResumeIDReporter is implemented by adapters that learn the agent's
// conversation id while it runs.
type ResumeIDReporter interface {
	// ResumeID is the latest valid id reported (lowercase), "" if none yet.
	ResumeID() string
}

// WaitReasoner is implemented by adapters that know why a needs_input
// session waits.
type WaitReasoner interface {
	// WaitReason is protocol.WaitReview or protocol.WaitInput, "" when
	// unknown or while the agent works.
	WaitReason() string
}

// WaitReporter is implemented by adapters that know when a working agent is
// blocked on a tool (a shell command, a subagent, a web call).
type WaitReporter interface {
	// Waiting is the kind (protocol.WaitShell and friends) and the start of
	// the wait, or "" and the zero time when the agent is not waiting on a
	// tool. It is empty unless the agent's state is working.
	Waiting() (kind string, since time.Time)
}

// InterruptObserver is implemented by adapters that use the user's
// interrupt keys (Esc, Ctrl+C written to the session) as a signal.
type InterruptObserver interface {
	// UserInterrupt is called when such a key is forwarded to the agent.
	UserInterrupt()
}

// PromptCounter is implemented by adapters that count the user's prompts.
type PromptCounter interface {
	// Prompts is how many prompts the user sent; it never goes down.
	// 0 means none seen (or the agent does not report them).
	Prompts() int
}

// ClaudeHooks classifies Claude Code sessions from the hook state file written
// by ClaudeHookArgs hooks. Claude renders a full-screen TUI to the PTY (its
// stream-json output only exists with --print), so screen-scraping is
// unreliable; hook events are exact.
//
// The state model (effective): the main thread is in a turn, parked (its
// turn ended: Stop, StopFailure, SessionStart, an interrupt) or asking a
// question. READY (needs_input) means the user is needed:
//
//   - a question: at once;
//   - parked, with no subagent running and no subagent report owed (or the
//     report not delivered within wakeGrace);
//   - in a turn, the stale fallback: no hook and a still screen for
//     hookStaleWorking (subagentQuiet with a tool wait open), never while a
//     subagent runs.
//
// Everything else is working; parked on subagents it waits on a subagent.
type ClaudeHooks struct {
	path string

	mu      sync.Mutex
	off     int64  // bytes of the file already consumed
	partial []byte // a trailing line not yet terminated by '\n'
	sid     string // latest valid session_id from a sid line
	prompts int    // UserPromptSubmit count; survives a truncated file

	// question is set by a Notification that waits on the user and cleared
	// by the next working word (the answer let the agent go on), a
	// main-thread line or a prompt.
	question   bool
	questionBy string // the subagent asking; "" for the main thread (or unknown)
	// parked is set when the main turn ends (a needs_input word, an
	// interrupt) and cleared by the next main-thread tool line or prompt:
	// subagents running while it is set were left in the background, and
	// the session waits on them.
	parked      bool
	parkedSince time.Time // shown as the wait's start (a Task wait's start when the turn ended on one)
	parkedAt    time.Time // when the turn ended: the wake grace counts from here
	endReason   string    // wait reason of a parked session: review after a Stop, "" otherwise
	// waitOn and waitSince are the main thread's current tool wait (wait+
	// line); one slot, last writer wins.
	waitOn    string
	waitSince time.Time
	// waitEnding is set when a wait- arrived while subagents still ran:
	// the wait stays (several Task calls fan out) until the last one stops.
	waitEnding bool
	// subWaitSince is the start of a subagent tool wait ended by a
	// needs_input word; the Stop that follows parks from it.
	subWaitSince time.Time
	// escAt is when the user last pressed an interrupt key (UserInterrupt),
	// zero when none is pending. Any main-thread hook line clears it: the
	// turn went on, or a new one started.
	escAt time.Time

	agents map[string]time.Time // running subagents by agent_id: last sign of life
	// fg holds the running subagents started by the open Task wait (a
	// foreground subagent). It is cleared when that wait ends for good
	// (clearWait) or returns at once (wait- launch): a subagent still
	// running then is a background subagent.
	fg map[string]bool
	// nested holds running subagents started by a subagent (nest+); they
	// report to their parent, not to the main agent.
	nested      map[string]bool
	nestPending int       // nest+ lines whose subagent has not started yet
	nestUntil   time.Time // nestPending expires then (the call never started one)
	// bgLimit is the count of running background subagents the last Stop
	// reported (bg-agents), -1 when unknown. While parked, subagents above
	// it that are quiet for reconcileQuiet stop counting.
	bgLimit int
	// ended holds subagents whose SubagentStop came, so a late sign of life
	// (agent~) does not count them again; a new start (agent+) does.
	ended map[string]bool
	// A background subagent that ends owes the main agent a report, which
	// arrives as a UserPromptSubmit (prompt line). wakes counts those that
	// ended while parked (any main-thread line pays them all: the main
	// agent woke); reports those that ended during a turn (each prompt pays
	// one). lastEnd is the latest such end.
	wakes, reports int
	lastEnd        time.Time

	why string // last logged reason of the effective state
	now func() time.Time
}

// NewClaudeHooks reads hook state from path.
func NewClaudeHooks(path string) *ClaudeHooks {
	return &ClaudeHooks{
		path: path, agents: map[string]time.Time{}, fg: map[string]bool{},
		nested: map[string]bool{}, ended: map[string]bool{}, bgLimit: -1, now: time.Now,
	}
}

// Detect implements Adapter. window is unused; idle guards against a turn that
// ended without a Stop hook. See ClaudeHooks for the rules.
func (a *ClaudeHooks) Detect(_ []byte, idle time.Duration) protocol.State {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sync()
	st, _, _, _, why := a.effective(idle)
	if why != a.why {
		a.why = why
		slog.Info("adapter: claude state", "path", a.path, "state", st, "why", why,
			"subagents", len(a.agents), "owed", a.wakes+a.reports)
	}
	return st
}

// effective is the session's state, tool wait and wait reason, and why.
// Callers hold mu and ran sync.
func (a *ClaudeHooks) effective(idle time.Duration) (st protocol.State, kind string, since time.Time, reason, why string) {
	running := a.runningAgents()
	if a.question && a.questionBy != "" {
		if _, ok := a.agents[a.questionBy]; !ok {
			a.clearQuestion() // the asking subagent is gone
		}
	}
	switch {
	case a.question:
		return protocol.StateNeedsInput, "", time.Time{}, protocol.WaitInput, "question"
	case a.parked && running > 0:
		kind, since = a.parkedWait()
		return protocol.StateWorking, kind, since, "", "parked: subagents running"
	case a.parked && a.wakes+a.reports > 0:
		from := a.parkedAt
		if a.lastEnd.After(from) {
			from = a.lastEnd
		}
		if a.now().Sub(from) < wakeGrace {
			kind, since = a.parkedWait()
			return protocol.StateWorking, kind, since, "", "parked: subagent report owed"
		}
		// The report did not come (or came batched): the turn is over.
		a.wakes, a.reports = 0, 0
		fallthrough
	case a.parked:
		return protocol.StateNeedsInput, "", time.Time{}, a.endReason, "turn ended"
	}
	kind = a.openWait()
	if kind != "" {
		since = a.waitSince
	}
	if running > 0 {
		return protocol.StateWorking, kind, since, "", "turn: subagents running"
	}
	bound := hookStaleWorking
	if kind != "" {
		bound = subagentQuiet
	}
	if idle >= bound {
		a.clearWait()
		return protocol.StateNeedsInput, "", time.Time{}, "", "stale"
	}
	return protocol.StateWorking, kind, since, "", "turn"
}

// sync reads new hook lines and ends a turn the user interrupted. Callers
// hold mu.
func (a *ClaudeHooks) sync() {
	a.read()
	if a.escAt.IsZero() || a.now().Sub(a.escAt) < escBound {
		return
	}
	esc := a.escAt
	a.escAt = time.Time{}
	if a.parked || a.question {
		return // a question or a finished turn: nothing to end
	}
	a.interrupt(esc)
}

// interrupt ends the main turn the user interrupted at esc. A foreground
// subagent (started by the open Task wait) that showed no life since esc
// was killed with it, often with no SubagentStop. Any other subagent runs
// on in the background: the session parks on it, as after a Stop, so it
// is WAITING until the last one ends and READY once. Callers hold mu.
func (a *ClaudeHooks) interrupt(esc time.Time) {
	for id := range a.fg {
		if seen, ok := a.agents[id]; ok && seen.Before(esc) {
			delete(a.agents, id) // not endAgent: a killed subagent owes no report
			delete(a.nested, id)
		}
	}
	a.clearWait()
	slog.Info("adapter: claude turn interrupted", "path", a.path, "subagents", len(a.agents))
	a.endReason, a.subWaitSince = protocol.WaitReview, time.Time{}
	a.park(esc)
}

// UserInterrupt implements InterruptObserver: the user pressed Esc or
// Ctrl+C in the session. Lines already written are read first, so only a
// main-thread line written after the key cancels it.
func (a *ClaudeHooks) UserInterrupt() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.read()
	if a.parked || a.question {
		return // the main turn already ended: nothing to interrupt
	}
	a.escAt = a.now()
}

// park marks the main turn ended at since (shown as the wait's start).
// Callers hold mu.
func (a *ClaudeHooks) park(since time.Time) {
	if a.parked {
		return
	}
	a.parked, a.parkedSince, a.parkedAt = true, since, since
}

// clearQuestion ends a pending question. Callers hold mu.
func (a *ClaudeHooks) clearQuestion() {
	a.question, a.questionBy = false, ""
}

// answeredByMain ends the question if the main thread asked it (or the
// asker is unknown); a subagent's question waits for that subagent.
// Callers hold mu.
func (a *ClaudeHooks) answeredByMain() {
	if a.questionBy == "" {
		a.clearQuestion()
	}
}

// unpark ends a parked session's subagent wait. Callers hold mu.
func (a *ClaudeHooks) unpark() {
	a.parked, a.parkedSince, a.parkedAt, a.subWaitSince = false, time.Time{}, time.Time{}, time.Time{}
	a.bgLimit = -1
}

// mainLine records main-thread activity: a parked main agent woke (paying
// the reports owed while parked), a question was answered, a pending
// interrupt did not end the turn. Callers hold mu.
func (a *ClaudeHooks) mainLine() {
	if a.parked {
		a.wakes = 0
	}
	a.unpark()
	a.answeredByMain()
	a.endReason = ""
	a.escAt = time.Time{}
}

// endTurn handles a needs_input word: the main turn ended. Callers hold mu.
func (a *ClaudeHooks) endTurn() {
	if a.waitOn == protocol.WaitSubagent {
		a.subWaitSince = a.waitSince // a Stop parks from here
	}
	a.clearWait()
	a.answeredByMain() // the turn is over: so is the main thread's question
	a.park(a.now())
}

// openWait is the main thread's tool wait, ending a fan-out wait whose last
// subagent went quiet. Callers hold mu.
func (a *ClaudeHooks) openWait() string {
	if a.waitEnding && a.runningAgents() == 0 {
		a.clearWait()
	}
	return a.waitOn
}

// parkedWait is the wait shown for a session held working by its
// subagents: on a subagent, since the main turn ended. Callers hold mu.
func (a *ClaudeHooks) parkedWait() (string, time.Time) {
	since := a.parkedSince
	if since.IsZero() {
		since = a.now()
		a.parkedSince = since
	}
	return protocol.WaitSubagent, since
}

// Waiting implements WaitReporter.
func (a *ClaudeHooks) Waiting() (string, time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sync()
	st, kind, since, _, _ := a.effective(0)
	if st != protocol.StateWorking || kind == "" {
		return "", time.Time{}
	}
	return kind, since
}

// clearWait ends the tool wait. Callers hold mu.
func (a *ClaudeHooks) clearWait() {
	a.waitOn, a.waitSince, a.waitEnding = "", time.Time{}, false
	clear(a.fg)
}

// runningAgents counts subagents, dropping any quiet for subagentQuiet.
// Callers hold mu.
func (a *ClaudeHooks) runningAgents() int {
	cutoff := a.now().Add(-subagentQuiet)
	for id, seen := range a.agents {
		if seen.Before(cutoff) {
			a.endAgent(id, false)
		}
	}
	if a.parked && a.bgLimit >= 0 {
		a.reconcile()
	}
	return len(a.agents)
}

// reconcileQuiet is how long a subagent above the Stop's background_tasks
// count must be quiet before it stops counting. A busy subagent shows life
// sooner, so a miscounted Stop cannot drop it.
const reconcileQuiet = time.Minute

// endedMax bounds the ended set; past it the set starts over (a late sign
// of life for a long-ended subagent is harmless: it goes quiet again).
const endedMax = 256

// endAgent removes a running subagent. stopped is true for a SubagentStop
// (a later sign of life is ignored); false for a subagent presumed gone
// (quiet, or missing from the Stop's background_tasks). A background
// subagent owes the main agent a report. Callers hold mu.
func (a *ClaudeHooks) endAgent(id string, stopped bool) {
	if _, ok := a.agents[id]; !ok {
		return
	}
	background := !a.fg[id] && !a.nested[id]
	delete(a.agents, id)
	delete(a.fg, id)
	delete(a.nested, id)
	if stopped {
		if len(a.ended) >= endedMax {
			clear(a.ended)
		}
		a.ended[id] = true
	}
	if !background {
		return
	}
	a.lastEnd = a.now()
	if a.parked {
		a.wakes++
	} else {
		a.reports++
	}
}

// reconcile drops running subagents above bgLimit, the count of running
// background subagents Claude reported at the Stop: their SubagentStop was
// lost (a crash, a kill). The ones with the oldest sign of life go first,
// once quiet for reconcileQuiet. A count above the known set changes
// nothing. Callers hold mu.
func (a *ClaudeHooks) reconcile() {
	cutoff := a.now().Add(-reconcileQuiet)
	for len(a.agents) > a.bgLimit {
		var oldest string
		for id, seen := range a.agents {
			if oldest == "" || seen.Before(a.agents[oldest]) {
				oldest = id
			}
		}
		if !a.agents[oldest].Before(cutoff) {
			return
		}
		slog.Info("adapter: claude subagent not in background_tasks", "path", a.path, "agent", oldest)
		a.endAgent(oldest, false)
	}
}

// Prompts implements PromptCounter.
func (a *ClaudeHooks) Prompts() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.read()
	return a.prompts
}

// Subagents implements SubagentCounter: subagents started and not yet
// stopped, minus any quiet for subagentQuiet. A set, not a counter, because
// resuming a subagent fires SubagentStart again with the same agent_id.
func (a *ClaudeHooks) Subagents() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.read()
	return a.runningAgents()
}

// ResumeID implements ResumeIDReporter. The id survives a truncated hook
// file (a resumed session reuses its conversation until a new sid arrives).
func (a *ClaudeHooks) ResumeID() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.read()
	return a.sid
}

// WaitReason implements WaitReasoner: input for a question, review for a
// turn that ended with a Stop (or an interrupt), "" otherwise and while
// the session works.
func (a *ClaudeHooks) WaitReason() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sync()
	_, _, _, reason, _ := a.effective(0)
	return reason
}

// IsReadyForInput implements Adapter. Claude receives its first prompt on argv,
// so the PTY paste path is never used.
func (a *ClaudeHooks) IsReadyForInput(_ []byte, _ time.Duration) bool {
	return false
}

// hookReadMax bounds one read of new hook lines.
const hookReadMax = 1 << 20

// read consumes the lines appended since the last call. Callers hold mu.
func (a *ClaudeHooks) read() {
	f, err := os.Open(a.path)
	if err != nil {
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return
	}
	size := info.Size()
	if size < a.off { // truncated or replaced: start over
		a.off, a.partial = 0, nil
		a.resetAgents()
		a.clearWait()
		a.unpark()
		a.clearQuestion()
		a.endReason = ""
		a.escAt = time.Time{}
	}
	if size == a.off {
		return
	}
	buf := make([]byte, min(size-a.off, hookReadMax))
	got, err := f.ReadAt(buf, a.off)
	if err != nil && !errors.Is(err, io.EOF) {
		return
	}
	a.off += int64(got) // a file shrunk since Stat reads short
	// Every hook writes its lines with one small printf, so a partial line
	// is a write still in flight and its rest arrives next.
	buf = append(a.partial, buf[:got]...)
	end := bytes.LastIndexByte(buf, '\n')
	if end < 0 {
		a.partial = buf
		return
	}
	a.partial = append([]byte(nil), buf[end+1:]...)
	lines := strings.Split(string(buf[:end]), "\n")
	for i, line := range lines {
		next := ""
		if i+1 < len(lines) {
			next, _, _ = strings.Cut(strings.TrimSpace(lines[i+1]), " ")
		}
		a.apply(strings.TrimSpace(line), next)
	}
}

// resetAgents forgets every subagent and owed report. Callers hold mu.
func (a *ClaudeHooks) resetAgents() {
	clear(a.agents)
	clear(a.fg)
	clear(a.nested)
	clear(a.ended)
	a.nestPending, a.wakes, a.reports, a.lastEnd = 0, 0, 0, time.Time{}
	a.bgLimit = -1
}

// nestMax bounds pending nest+ lines; nestExpire drops them when the
// subagent's Agent call started none (denied, failed).
const (
	nestMax    = 64
	nestExpire = 10 * time.Second
)

// apply takes one hook line: a state word or a subagent line. next is the
// first word of the line after it in the same read, "" if none. Each hook
// writes its lines in one printf, so a line its own hook always follows
// with another (needs_input, working) is told apart by that next word.
func (a *ClaudeHooks) apply(line, next string) {
	word, arg, _ := strings.Cut(line, " ")
	switch word {
	case "":
		return
	case string(protocol.StateWorking):
		a.answeredByMain() // an answered question lets the agent go on
		a.subWaitSince = time.Time{}
		// A working word on its own (not followed by its hook's subagent
		// or main-thread line) is a subagent's PreToolUse, or the main
		// thread of an older hook file. With no subagent known to run, a
		// parked main agent woke.
		switch next {
		case hookAgentSeen, hookNest, hookWaitStart, hookWaitEnd, hookPrompt:
		default:
			if a.parked && a.runningAgents() == 0 {
				a.mainLine()
			}
		}
	case string(protocol.StateNeedsInput):
		if next == hookNotify {
			return // a Notification: its type decides (notifyClass)
		}
		a.endTurn() // Stop, StopFailure, SessionStart
	case hookNotify:
		switch notifyClass(arg) {
		case notifyQuestion:
			a.escAt = time.Time{}
			a.question, a.questionBy = true, "" // the tool wait stays: it goes on after the answer
		case notifyTurnEnd:
			a.escAt = time.Time{}
			a.endTurn()
		default:
			slog.Info("adapter: claude notification ignored", "path", a.path, "type", arg)
		}
	case hookWaitStart:
		a.mainLine()
		// An unknown kind is ignored: the file is plain text.
		if protocol.ValidWaitKind(arg) {
			a.waitOn, a.waitSince, a.waitEnding = arg, a.now(), false
		}
	case hookWaitEnd:
		a.mainLine()
		switch {
		case arg == waitLaunch:
			// The Agent call returned at once: its subagents run in the
			// background.
			a.clearWait()
		case a.waitOn == protocol.WaitSubagent && a.runningAgents() > 0:
			// One of several parallel Task calls ending leaves the wait on
			// while other subagents run; the last stop (or a quiet
			// timeout) ends it.
			a.waitEnding = true
		default:
			a.clearWait()
		}
	case hookAsker:
		if a.question {
			a.questionBy = arg
		}
	case hookNest:
		a.nestPending = min(a.nestPending+1, nestMax)
		a.nestUntil = a.now().Add(nestExpire)
	case hookAgentStart:
		if arg == "" {
			return
		}
		delete(a.ended, arg) // a resumed subagent runs again
		if _, ok := a.agents[arg]; !ok {
			if a.nestPending > 0 && !a.now().Before(a.nestUntil) {
				a.nestPending = 0 // the nested calls never started one
			}
			switch {
			case a.nestPending > 0:
				a.nestPending--
				a.nested[arg] = true
			case a.waitOn == protocol.WaitSubagent && !a.waitEnding:
				a.fg[arg] = true
			}
		}
		a.agents[arg] = a.now()
	case hookAgentSeen:
		// A sign of life also brings back a subagent dropped as quiet or
		// cleared by a reset while it ran, but not one that stopped.
		if arg != "" && !a.ended[arg] {
			a.agents[arg] = a.now()
		}
		if a.question && arg == a.questionBy {
			a.clearQuestion() // the asking subagent goes on: answered
		}
	case hookAgentStop:
		a.endAgent(arg, true)
		if a.waitEnding && len(a.agents) == 0 {
			a.clearWait()
		}
	case hookAgentsReset:
		a.resetAgents()
		a.escAt = time.Time{}
		a.clearQuestion()
		a.endReason = ""
		a.unpark()
	case hookCompacted:
		slog.Debug("adapter: claude compacted", "path", a.path)
	case hookTurnDone:
		a.escAt = time.Time{}
		a.answeredByMain()
		a.nestPending = 0
		a.endReason = protocol.WaitReview
		a.park(a.now())
		if !a.subWaitSince.IsZero() { // the subagents were started by a Task wait
			a.parkedSince, a.subWaitSince = a.subWaitSince, time.Time{}
		}
	case hookBgAgents:
		if n, err := strconv.Atoi(arg); err == nil && n >= 0 && a.parked {
			a.bgLimit = n
		}
	case hookPrompt:
		// A prompt reached the main agent: the user's, or a background
		// subagent's report. It pays one owed report.
		if a.parked && a.wakes > 0 {
			a.wakes = 0
		} else if a.reports > 0 {
			a.reports--
		}
		a.mainLine()
		a.clearQuestion() // the user typed: any question is over
		a.clearWait()
		a.nestPending = 0
		a.prompts++
	case hookSessionID:
		// The latest wins (/clear and in-app /resume start new conversations).
		// An invalid or missing id is ignored: it ends up on an argv.
		if id, ok := protocol.NormalizeResumeID(arg); ok {
			a.sid = id
		}
	default:
		slog.Warn("adapter: unknown claude hook line", "path", a.path, "line", line)
	}
}
