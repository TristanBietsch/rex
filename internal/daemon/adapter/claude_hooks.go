package adapter

import (
	"bytes"
	"encoding/json"
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
// SessionStart also resets the subagent set (a new or cleared conversation).
var claudeHookStates = []struct {
	event   string
	matcher bool // tool events take a matcher
	state   protocol.State
	reset   bool // write hookAgentsReset first
}{
	{"SessionStart", false, protocol.StateNeedsInput, true},
	{"UserPromptSubmit", false, protocol.StateWorking, false},
	{"PreToolUse", true, protocol.StateWorking, false},
	{"PostToolUse", true, protocol.StateWorking, false},
	{"Notification", false, protocol.StateNeedsInput, false}, // permission / idle prompt
	{"Stop", false, protocol.StateNeedsInput, false},         // turn finished, waiting on user
}

// Subagent lines in the hook file, next to the state words:
// "agent+ <agent_id>" from SubagentStart, "agent- <agent_id>" from
// SubagentStop, and "agents-reset" from SessionStart.
const (
	hookAgentStart  = "agent+"
	hookAgentStop   = "agent-"
	hookAgentsReset = "agents-reset"
)

// subagentHookCommand writes "<word> <agent_id>" for a SubagentStart or
// SubagentStop event. It reads agent_id and agent_type from the hook's JSON
// on stdin with sed (no jq dependency). An event without an agent_type is
// one of Claude's internal helpers (prompt suggestions, /btw), not a
// subagent the user started, and is skipped. The id is cut to a safe
// character set before it reaches the file.
func subagentHookCommand(word string) string {
	field := func(name string) string {
		return `printf '%s' "$in" | sed -n 's/.*"` + name + `" *: *"\([^"]*\)".*/\1/p' | head -n 1`
	}
	return `in=$(cat); id=$(` + field("agent_id") + ` | tr -cd 'A-Za-z0-9_.:-'); ty=$(` + field("agent_type") + `); ` +
		`if [ -n "$id" ] && [ -n "$ty" ]; then printf '` + word + ` %s\n' "$id" >> "$` + HookFileEnv + `"; fi 2>/dev/null; exit 0`
}

// ClaudeHookArgs returns argv that makes Claude Code append one state word per
// hook event to the file named by $REX_HOOK_FILE, plus a line per subagent
// start and stop. Hooks always exit 0 so a write failure can never block the
// agent.
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
		lines := string(h.state) + `\n`
		if h.reset {
			lines = hookAgentsReset + `\n` + lines // one write keeps the order
		}
		g := matcherGroup{Hooks: []hookCmd{{
			Type:    "command",
			Command: `printf '` + lines + `' >> "$` + HookFileEnv + `" 2>/dev/null; exit 0`,
		}}}
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
	agents  map[string]struct{} // running subagents by agent_id
}

// NewClaudeHooks reads hook state from path.
func NewClaudeHooks(path string) *ClaudeHooks {
	return &ClaudeHooks{path: path, last: protocol.StateWorking, agents: map[string]struct{}{}}
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
// stopped. A set, not a counter, because resuming a subagent fires
// SubagentStart again with the same agent_id.
func (a *ClaudeHooks) Subagents() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.read()
	return len(a.agents)
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
	n := min(size-a.off, hookReadMax)
	buf := make([]byte, n)
	if _, err := f.ReadAt(buf, a.off); err != nil && err != io.EOF {
		return
	}
	a.off += n
	buf = append(a.partial, buf...)
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
	case hookAgentStart:
		if arg != "" {
			a.agents[arg] = struct{}{}
		}
	case hookAgentStop:
		delete(a.agents, arg)
	case hookAgentsReset:
		clear(a.agents)
	default:
		slog.Warn("adapter: unknown claude hook line", "path", a.path, "line", line)
	}
}
