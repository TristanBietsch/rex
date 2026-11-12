package adapter

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
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
const hookStaleWorking = 30 * time.Second

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
}

// Subagent lines in the hook file, next to the state words:
//
//	agent+ <agent_id>  SubagentStart
//	agent- <agent_id>  SubagentStop
//	agent~ <agent_id>  PostToolUse of a subagent's tool call (still running)
//	agents-reset       SessionStart of a new or cleared conversation
//	sid <session_id>   SessionStart: the conversation id (resume id)
//	turn-done          Stop: the agent finished a turn (wait_reason review)
//	notify <type>      Notification: notification_type, "" if absent
//	prompt             UserPromptSubmit: the user sent a prompt (a new turn)
//	wait+ <kind>       PreToolUse of a main-thread shell, subagent or web
//	                   tool (kind: shell, subagent, web)
//	wait-              PostToolUse of a main-thread tool call
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
)

// notifyIdle is the Notification type Claude fires after about 60 seconds
// idle. It must not turn a finished turn into a question.
const notifyIdle = "idle_prompt"

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

// claudeHookCommand is the command for one state event. SessionStart also
// resets the subagent set, except after a compaction (subagents may still
// be running), and reports the conversation's session_id (a `sid` line in
// the same printf, so it is atomic with the state word). PostToolUse also marks a subagent's tool call (the JSON has
// an agent_id) as a sign of life.
func claudeHookCommand(event string, state protocol.State) string {
	st := string(state)
	switch event {
	case "SessionStart":
		return `in=$(cat); src=$(` + hookJSONField("source") + `); sid=$(` + hookJSONField("session_id") + hookSafeID + `); ` +
			hookAppend(`if [ "$src" = compact ]; then printf '`+st+`\n`+hookSessionID+` %s\n' "$sid"; else printf '`+hookAgentsReset+`\n`+st+`\n`+hookSessionID+` %s\n' "$sid"; fi`)
	case "PostToolUse", "PostToolUseFailure":
		return `in=$(cat); id=; case "$in" in *'"agent_id"'*) id=$(` + hookJSONField("agent_id") + hookSafeID + `);; esac; ` +
			hookAppend(`if [ -n "$id" ]; then printf '`+st+`\n`+hookAgentSeen+` %s\n' "$id"; else printf '`+st+`\n`+hookWaitEnd+`\n'; fi`)
	case "Stop":
		return hookAppend(`printf '` + st + `\n` + hookTurnDone + `\n'`)
	case "Notification":
		return `in=$(cat); ty=$(` + hookJSONField("notification_type") + hookSafeID + `); ` +
			hookAppend(`printf '`+st+`\n`+hookNotify+` %s\n' "$ty"`)
	case "PreToolUse":
		// Only a main-thread call (no agent_id) can mark the session as
		// waiting. The kind is picked by a shell case, so only the three
		// fixed words are ever written.
		return `in=$(cat); id=; case "$in" in *'"agent_id"'*) id=$(` + hookJSONField("agent_id") + hookSafeID + `);; esac; k=; ` +
			`if [ -z "$id" ]; then case "$(` + hookJSONField("tool_name") + `)" in Bash) k=` + protocol.WaitShell +
			`;; Task|Agent) k=` + protocol.WaitSubagent + `;; WebFetch|WebSearch) k=` + protocol.WaitWeb + `;; esac; fi; ` +
			hookAppend(`if [ -n "$id" ]; then printf '`+st+`\n'; elif [ -n "$k" ]; then printf '`+st+`\n`+hookWaitStart+` %s\n' "$k"; else printf '`+st+`\n`+hookWaitEnd+`\n'; fi`)
	case "UserPromptSubmit":
		// A new turn: no tool of the last one is still being waited on.
		return hookAppend(`printf '` + st + `\n` + hookWaitEnd + `\n` + hookPrompt + `\n'`)
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
type ClaudeHooks struct {
	path string

	mu      sync.Mutex
	off     int64  // bytes of the file already consumed
	partial []byte // a trailing line not yet terminated by '\n'
	last    protocol.State
	agents  map[string]time.Time // running subagents by agent_id: last sign of life
	sid     string               // latest valid session_id from a sid line
	reason  string               // why the agent waits (protocol.Wait*), "" if unknown
	// waitOn and waitSince are the main thread's current tool wait (wait+
	// line); one slot, last writer wins.
	waitOn    string
	waitSince time.Time
	// waitEnding is set when a wait- arrived while subagents still ran:
	// the wait stays (several Task calls fan out) until the last one stops.
	waitEnding bool
	// parked is set when the main turn ends (Stop) and cleared by the next
	// main-thread tool line or prompt: subagents running while it is set
	// were left in the background, and the session waits on them.
	parked      bool
	parkedSince time.Time
	// wakeAt is when the last subagent of a parked session ended; zero when
	// no wake is expected. Any state word clears it.
	wakeAt  time.Time
	prompts int // UserPromptSubmit count; survives a truncated file
	now     func() time.Time
}

// NewClaudeHooks reads hook state from path.
func NewClaudeHooks(path string) *ClaudeHooks {
	return &ClaudeHooks{path: path, last: protocol.StateWorking, agents: map[string]time.Time{}, now: time.Now}
}

// Detect implements Adapter. window is unused; idle guards against a turn that
// ended without a Stop hook.
//
// READY (needs_input) means the agent waits on the user: a question
// (Notification other than idle_prompt) at once; a Stop or idle Notification
// only once no subagent runs and no wake is pending; a working word only
// after the stale fallback, which never fires while a subagent runs.
func (a *ClaudeHooks) Detect(_ []byte, idle time.Duration) protocol.State {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.read()
	if a.last == protocol.StateNeedsInput {
		if a.subagentHold() {
			return protocol.StateWorking
		}
		return protocol.StateNeedsInput
	}
	if a.runningAgents() > 0 || a.waking() {
		return protocol.StateWorking
	}
	bound := hookStaleWorking
	if a.openWait() != "" {
		bound = subagentQuiet
	}
	if idle >= bound {
		a.clearWait()
		return protocol.StateNeedsInput
	}
	return protocol.StateWorking
}

// subagentHold reports whether a needs_input word is held as working because
// subagents still run (or one just ended and the main agent should wake). A
// question is never held. Callers hold mu.
func (a *ClaudeHooks) subagentHold() bool {
	if a.reason == protocol.WaitInput {
		return false
	}
	return a.runningAgents() > 0 || a.waking()
}

// waking reports whether a parked session's last subagent ended less than
// wakeGrace ago. Callers hold mu.
func (a *ClaudeHooks) waking() bool {
	return !a.wakeAt.IsZero() && a.now().Sub(a.wakeAt) < wakeGrace
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
	a.read()
	if a.last == protocol.StateNeedsInput {
		if a.subagentHold() {
			return a.parkedWait()
		}
		return "", time.Time{}
	}
	if kind := a.openWait(); kind != "" {
		return kind, a.waitSince
	}
	if a.parked && (a.runningAgents() > 0 || a.waking()) {
		return a.parkedWait()
	}
	return "", time.Time{}
}

// clearWait ends the tool wait. Callers hold mu.
func (a *ClaudeHooks) clearWait() {
	a.waitOn, a.waitSince, a.waitEnding = "", time.Time{}, false
}

// runningAgents counts subagents, dropping any quiet for subagentQuiet.
// Callers hold mu.
func (a *ClaudeHooks) runningAgents() int {
	cutoff := a.now().Add(-subagentQuiet)
	for id, seen := range a.agents {
		if seen.Before(cutoff) {
			a.dropAgent(id)
		}
	}
	return len(a.agents)
}

// dropAgent removes a running subagent. The last one of a parked session
// leaving starts the wake grace. Callers hold mu.
func (a *ClaudeHooks) dropAgent(id string) {
	if _, ok := a.agents[id]; !ok {
		return
	}
	delete(a.agents, id)
	if len(a.agents) == 0 && a.parked {
		a.wakeAt = a.now()
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

// WaitReason implements WaitReasoner: review after a Stop, input after a
// Notification other than idle_prompt, which keeps what was there. It is ""
// while the session is held working by its subagents.
func (a *ClaudeHooks) WaitReason() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.read()
	if a.last == protocol.StateNeedsInput && a.subagentHold() {
		return ""
	}
	return a.reason
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
		clear(a.agents)
		a.clearWait()
		a.parked, a.wakeAt = false, time.Time{}
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
	for _, line := range bytes.Split(buf[:end], []byte("\n")) {
		a.apply(string(bytes.TrimSpace(line)))
	}
}

// apply takes one hook line: a state word or a subagent line.
func (a *ClaudeHooks) apply(line string) {
	word, arg, _ := strings.Cut(line, " ")
	switch word {
	case "":
		return
	case string(protocol.StateWorking), string(protocol.StateNeedsInput):
		a.last = protocol.State(word)
		a.wakeAt = time.Time{} // the main agent woke (or stopped again)
		if a.last == protocol.StateWorking {
			a.reason = ""
		} else { // Stop, Notification, SessionStart end any tool wait
			a.clearWait()
		}
		slog.Debug("adapter: claude hook state", "path", a.path, "state", a.last)
	case hookWaitStart:
		// An unknown kind is ignored: the file is plain text.
		a.parked = false // main-thread activity
		if protocol.ValidWaitKind(arg) {
			a.waitOn, a.waitSince, a.waitEnding = arg, a.now(), false
		}
	case hookWaitEnd:
		a.parked = false // main-thread activity
		// One of several parallel Task calls ending leaves the wait on
		// while other subagents run; the last stop (or a quiet timeout)
		// ends it.
		if a.waitOn == protocol.WaitSubagent && a.runningAgents() > 0 {
			a.waitEnding = true
		} else {
			a.clearWait()
		}
	case hookAgentStart, hookAgentSeen:
		// A sign of life also brings back a subagent dropped as quiet or
		// cleared by a reset while it ran.
		if arg != "" {
			a.agents[arg] = a.now()
		}
	case hookAgentStop:
		a.dropAgent(arg)
		if a.waitEnding && len(a.agents) == 0 {
			a.clearWait()
		}
	case hookAgentsReset:
		clear(a.agents)
		a.reason = ""
		a.parked, a.wakeAt = false, time.Time{}
	case hookTurnDone:
		a.reason = protocol.WaitReview
		a.parked, a.parkedSince = true, a.now()
	case hookPrompt:
		a.prompts++
	case hookNotify:
		if arg != notifyIdle {
			a.reason = protocol.WaitInput
		}
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
