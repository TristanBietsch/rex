package adapter

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"os"
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

// ClaudeHookArgs returns argv that makes Claude Code append one state word per
// hook event to the file named by $REX_HOOK_FILE. Hooks always exit 0 so a
// write failure can never block the agent.
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
		g := matcherGroup{Hooks: []hookCmd{{
			Type:    "command",
			Command: `printf '` + string(h.state) + `\n' >> "$` + HookFileEnv + `" 2>/dev/null; exit 0`,
		}}}
		if h.matcher {
			g.Matcher = "*"
		}
		hooks[h.event] = append(hooks[h.event], g)
	}
	b, _ := json.Marshal(map[string]any{"hooks": hooks})
	return []string{"--settings", string(b)}
}

// ClaudeHooks classifies Claude Code sessions from the hook state file written
// by ClaudeHookArgs hooks. Claude renders a full-screen TUI to the PTY (its
// stream-json output only exists with --print), so screen-scraping is
// unreliable; hook events are exact.
type ClaudeHooks struct {
	path string

	mu       sync.Mutex
	lastSize int64
	last     protocol.State
}

// NewClaudeHooks reads hook state from path.
func NewClaudeHooks(path string) *ClaudeHooks {
	return &ClaudeHooks{path: path, last: protocol.StateWorking}
}

// Detect implements Adapter. window is unused; idle guards against a turn that
// ended without a Stop hook.
func (a *ClaudeHooks) Detect(_ []byte, idle time.Duration) protocol.State {
	a.mu.Lock()
	defer a.mu.Unlock()
	if st, ok := a.readLast(); ok {
		a.last = st
	}
	if a.last == protocol.StateWorking && idle >= hookStaleWorking {
		return protocol.StateNeedsInput
	}
	return a.last
}

// IsReadyForInput implements Adapter. Claude receives its first prompt on argv,
// so the PTY paste path is never used.
func (a *ClaudeHooks) IsReadyForInput(_ []byte, _ time.Duration) bool {
	return false
}

// readLast returns the state on the file's last line when the file grew since
// the previous read.
func (a *ClaudeHooks) readLast() (protocol.State, bool) {
	f, err := os.Open(a.path)
	if err != nil {
		return "", false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.Size() == a.lastSize {
		return "", false
	}
	a.lastSize = info.Size()
	const tailMax = 256
	off := info.Size() - tailMax
	if off < 0 {
		off = 0
	}
	buf := make([]byte, info.Size()-off)
	if _, err := f.ReadAt(buf, off); err != nil && err != io.EOF {
		return "", false
	}
	lines := bytes.Split(bytes.TrimSpace(buf), []byte("\n"))
	word := string(bytes.TrimSpace(lines[len(lines)-1]))
	switch st := protocol.State(word); st {
	case protocol.StateWorking, protocol.StateNeedsInput:
		slog.Debug("adapter: claude hook state", "path", a.path, "state", st)
		return st, true
	default:
		slog.Warn("adapter: unknown claude hook state", "path", a.path, "line", word)
		return "", false
	}
}
