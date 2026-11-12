package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
	"github.com/tristanbietsch/rex/internal/workspace/wsconfig"
)

// Settings that config.lua sets are locked: the TUI shows them but never
// changes them, since the value lives in the Lua file (R05 D3). Everything
// here is the one place that knows; the settings page, :theme, the spawn
// cascade and the setup wizard ask it.

// settingLockedAt is where config.lua sets id ("config.lua:12"), or "" when
// it does not (or there is no store).
func settingLockedAt(m Model, id string) string {
	if m.Store == nil || !m.Store.Locked(id) {
		return ""
	}
	return m.Store.SetAt(id)
}

// lockMessage is D3's refusal for a locked id.
func lockMessage(id, at string) string {
	return protocol.FlattenDisplayText(fmt.Sprintf("%s: set in %s — press e to open", id, at))
}

// settingLocked reports whether id is locked and, when it is, puts D3's
// message on the status line. Every settings write path calls it first.
func settingLocked(m *Model, id string) bool {
	at := settingLockedAt(*m, id)
	if at == "" {
		return false
	}
	m.Err = lockMessage(id, at)
	return true
}

// settingLockBadge is the muted marker a locked row shows next to its
// value, "" for other rows: "lua" for the user's config.lua, "ws" for a
// workspace's .rex/config.lua (at most 3 cells, the room the page keeps).
// It is the only lock rendering, so a settings page rewrite has one helper
// to port.
func settingLockBadge(m Model, s settings.Setting) string {
	at := settingLockedAt(m, s.ID)
	switch {
	case at == "":
		return ""
	case settings.WorkspaceSetAt(at):
		return styleMuted.Render("ws")
	}
	return styleMuted.Render("lua")
}

// luaFileOf maps a config.lua position ("config.lua:12", "lua/ui.lua:3", or
// a workspace's "/path/.rex/config.lua:3") to the file's path and line. ok
// is false for anything else.
func luaFileOf(at string) (path string, line int, ok bool) {
	i := strings.LastIndexByte(at, ':')
	if i <= 0 {
		return "", 0, false
	}
	line, err := strconv.Atoi(at[i+1:])
	if err != nil || line < 0 {
		return "", 0, false
	}
	rel := at[:i]
	// A workspace's .rex/config.lua: only the file of the workspace this
	// process applies, taken from wsconfig (never parsed back out of the
	// flattened position), and never a symlink.
	if settings.WorkspaceSetAt(at) {
		file := workspaceConfigFile()
		if file == "" || rel != protocol.FlattenDisplayText(file) {
			return "", 0, false
		}
		if fi, err := os.Lstat(file); err != nil || !fi.Mode().IsRegular() {
			return "", 0, false
		}
		return file, line, true
	}
	l, err := paths.Resolve()
	if err != nil {
		return "", 0, false
	}
	switch {
	case rel == "config.lua":
		return l.LuaConfigFile, line, true
	case strings.HasPrefix(rel, "lua/") && strings.HasSuffix(rel, ".lua"):
		parts := strings.Split(strings.TrimPrefix(rel, "lua/"), "/")
		for _, p := range parts {
			if p == "" || p == "." || p == ".." || strings.ContainsAny(p, `\:`) {
				return "", 0, false
			}
		}
		return filepath.Join(append([]string{l.LuaModuleDir}, parts...)...), line, true
	}
	return "", 0, false
}

// workspaceConfigFile is the .rex/config.lua path of the workspace this
// process applies ("" for none). Tests replace it.
var workspaceConfigFile = func() string { return wsconfig.Current().File }

// lineEditors take +N to open a file at line N.
var lineEditors = map[string]bool{"vi": true, "vim": true, "nvim": true, "nano": true, "emacs": true, "micro": true}

// luaEditorArgs is the argument list for editor to open file at line (+N for
// the editors known to take it).
func luaEditorArgs(editor, file string, line int) []string {
	base := strings.ToLower(editor)
	if i := strings.LastIndexAny(base, `/\`); i >= 0 {
		base = base[i+1:]
	}
	base = strings.TrimSuffix(base, ".exe")
	if line > 0 && lineEditors[base] {
		return []string{"+" + strconv.Itoa(line), file}
	}
	return []string{file}
}

// luaEditDoneMsg is the end of the $EDITOR run that `e` started.
type luaEditDoneMsg struct{ err error }

// openLockedInEditor runs $EDITOR on the file and line that lock id, as
// `rex config edit -lua` does. Not locked: nothing happens.
func openLockedInEditor(m Model, id string) (Model, tea.Cmd) {
	at := settingLockedAt(m, id)
	if at == "" {
		return m, nil
	}
	file, line, ok := luaFileOf(at)
	if !ok {
		m.Err = lockMessage(id, at)
		return m, nil
	}
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
		if runtime.GOOS == "windows" {
			editor = "notepad"
		}
	}
	child := exec.Command(editor, luaEditorArgs(editor, file, line)...) //nolint:gosec,noctx // G204: $EDITOR is the user's own program; noctx: it runs until the user quits it.
	return m, tea.ExecProcess(child, func(err error) tea.Msg { return luaEditDoneMsg{err: err} })
}

// handleLuaEditDone reloads the config after the editor exits, so the edit
// shows at once (whatever live_reload says).
func handleLuaEditDone(m Model, msg luaEditDoneMsg) (Model, tea.Cmd) {
	if msg.err != nil {
		m.Err = configNotice("editor", msg.err.Error(), "")
	}
	m.mouseOn = false // tea.ExecProcess turned mouse reporting off
	m, mc := syncMouse(m, "exec-return")
	if m.StorePath == "" {
		return m, mc
	}
	return m, tea.Batch(mc, loadConfig(m.StorePath, themesDir(), m.configStamp, true))
}
