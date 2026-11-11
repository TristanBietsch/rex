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
// visible PTY output. Claude animates a spinner/timer the whole time a turn
// runs, so silence this long means the turn ended without a Stop hook (user
// pressed Esc to interrupt).
const hookStaleWorking = 15 * time.Second

// claudeHookStates maps Claude Code hook events to the state each implies.
// SessionStart and PostToolUse also write subagent lines (see
// claudeHookCommand).
var claudeHookStates = []struct {
	event   string
	matcher bool // tool events take a matcher
	state   protocol.State
}{
	{"SessionStart", false, protocol.StateNeedsInput},
	{"UserPromptSubmit", false, protocol.StateWorking},
	{"PreToolUse", true, protocol.StateWorking},
	{"PostToolUse", true, protocol.StateWorking},
	{"Notification", false, protocol.StateNeedsInput}, // permission / idle prompt
	{"Stop", false, protocol.StateNeedsInput},         // turn finished, waiting on user
}

// Subagent lines in the hook file, next to the state words:
//
//	agent+ <agent_id>  SubagentStart
//	agent- <agent_id>  SubagentStop
//	agent~ <agent_id>  PostToolUse of a subagent's tool call (still running)
//	agents-reset       SessionStart of a new or cleared conversation
//	sid <session_id>   SessionStart: the conversation id (resume id)
const (
	hookAgentStart  = "agent+"
	hookAgentStop   = "agent-"
	hookAgentSeen   = "agent~"
	hookAgentsReset = "agents-reset"
	hookSessionID   = "sid"
)

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
	case "PostToolUse":
		return `in=$(cat); id=; case "$in" in *'"agent_id"'*) id=$(` + hookJSONField("agent_id") + hookSafeID + `);; esac; ` +
			hookAppend(`if [ -n "$id" ]; then printf '`+st+`\n`+hookAgentSeen+` %s\n' "$id"; else printf '`+st+`\n'; fi`)
	default:
		return hookAppend(`printf '` + st + `\n'`)
	}
}

// ClaudeHookArgs returns argv that makes Claude Code append one state word per
// hook event to the file named by $REX_HOOK_FILE, plus the subagent lines.
// Each hook writes its lines with one printf, so lines from hooks running in
// parallel never interleave.
func ClaudeHookArgs() []string {
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
	b, _ := json.Marshal(map[string]any{"hooks": hooks})
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
	now     func() time.Time
}

// NewClaudeHooks reads hook state from path.
func NewClaudeHooks(path string) *ClaudeHooks {
	return &ClaudeHooks{path: path, last: protocol.StateWorking, agents: map[string]time.Time{}, now: time.Now}
}

// Detect implements Adapter. window is unused; idle guards against a turn that
// ended without a Stop hook.
func (a *ClaudeHooks) Detect(_ []byte, idle time.Duration) protocol.State {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.read()
	if a.last == protocol.StateWorking && idle >= hookStaleWorking {
		return protocol.StateNeedsInput
	}
	return a.last
}

// Subagents implements SubagentCounter: subagents started and not yet
// stopped, minus any quiet for subagentQuiet. A set, not a counter, because
// resuming a subagent fires SubagentStart again with the same agent_id.
func (a *ClaudeHooks) Subagents() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.read()
	cutoff := a.now().Add(-subagentQuiet)
	for id, seen := range a.agents {
		if seen.Before(cutoff) {
			delete(a.agents, id)
		}
	}
	return len(a.agents)
}

// ResumeID implements ResumeIDReporter. The id survives a truncated hook
// file (a resumed session reuses its conversation until a new sid arrives).
func (a *ClaudeHooks) ResumeID() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.read()
	return a.sid
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
		slog.Debug("adapter: claude hook state", "path", a.path, "state", a.last)
	case hookAgentStart, hookAgentSeen:
		// A sign of life also brings back a subagent dropped as quiet or
		// cleared by a reset while it ran.
		if arg != "" {
			a.agents[arg] = a.now()
		}
	case hookAgentStop:
		delete(a.agents, arg)
	case hookAgentsReset:
		clear(a.agents)
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
