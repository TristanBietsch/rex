package tui

import (
	"fmt"
	"os"
	"reflect"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/catalog/theme"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// configPollEvery is how often the TUI checks config.yaml and the theme files
// for edits while live_reload is on.
const configPollEvery = 2 * time.Second

// configTickMsg is the poll timer.
type configTickMsg struct{}

// configReloadedMsg carries the result of re-reading config.yaml and the
// theme files. Update applies it on the UI goroutine; the Cmd that built it
// touched no shared state.
type configReloadedMsg struct {
	Store  *settings.Store // nil unless Changed and Err is nil
	Themes *theme.Catalog
	// Lua is what config.lua produced (zero while lua_config is off).
	Lua settings.LayerReport
	// Stamp is the state of the files as of the read.
	Stamp string
	// Changed is false for a poll that found the files as they were.
	Changed bool
	// Manual is true for :reload; it neither continues nor starts the poll.
	Manual bool
	Err    error
}

// configTick waits configPollEvery, then emits configTickMsg. It returns
// nothing once done closes (the TUI exited), so no timer goroutine outlives
// the program.
func configTick(done <-chan struct{}) tea.Cmd {
	return func() tea.Msg {
		t := time.NewTimer(configPollEvery)
		defer t.Stop()
		select {
		case <-t.C:
			return configTickMsg{}
		case <-done:
			return nil
		}
	}
}

// themesDir is the user's theme directory, "" when the home directory is
// unknown.
func themesDir() string {
	dir, err := paths.ThemesDir()
	if err != nil {
		return ""
	}
	return dir
}

// configStamp summarizes config.yaml, the theme files and, while luaOn
// (lua_config), config.lua and lua/: it changes when any of them is created,
// edited or removed. It stats paths, not inodes, so an editor that saves by
// rename is seen. With luaOn false config.lua is not stat'ed, and the stamp
// records that it is off (stampLuaOn).
func configStamp(path, themeDir string, luaOn bool) string {
	s := "-"
	if fi, err := os.Stat(path); err == nil {
		s = fmt.Sprintf("%d:%d", fi.Size(), fi.ModTime().UnixNano())
	}
	lua := luaOffStamp
	if luaOn {
		lua = "lua:" + settings.LayerStamp(true)
	}
	return s + "|" + theme.Stamp(themeDir) + "|" + lua
}

const luaOffStamp = "lua-off"

// stampLuaOn reports whether stamp was taken with lua_config on, so a poll
// stats config.lua only when the settings it last read had it on.
func stampLuaOn(stamp string) bool { return !strings.HasSuffix(stamp, "|"+luaOffStamp) }

// themesWithLua is the theme files plus the themes config.lua defined.
func themesWithLua(themeDir string, rep settings.LayerReport) *theme.Catalog {
	cat := theme.LoadDir(themeDir)
	if len(rep.Themes) == 0 && len(rep.ThemeIssues) == 0 {
		return cat
	}
	return cat.WithLua(rep.Themes, rep.ThemeIssues)
}

// maxNoticeRunes caps a config notice on the status line.
const maxNoticeRunes = 200

// configNotice is a status-line notice about file: one line, no escape,
// control or bidi rune. The text is cut so the whole notice, suffix
// included, is at most maxNoticeRunes runes.
func configNotice(file, text, suffix string) string {
	head := []rune(protocol.FlattenDisplayText(file + ": " + text))
	tail := []rune(protocol.FlattenDisplayText(suffix))
	if room := maxNoticeRunes - len(tail); len(head) > room {
		head = append(head[:max(room-3, 0)], []rune("...")...)
	}
	return string(head) + string(tail)
}

// luaNotice is the status-line notice for a config.lua that failed.
func luaNotice(rep settings.LayerReport) string {
	suffix := ""
	if rep.LastGood {
		suffix = " (kept its last good values)"
	}
	text := rep.Err.Error()
	if settings.WorkspaceSetAt(text) {
		// The workspace's .rex/config.lua: its error starts with the path.
		return configNotice("workspace config.lua", text, suffix)
	}
	return configNotice("config.lua", strings.TrimPrefix(text, "config.lua: "), suffix)
}

// loadConfig re-reads config.yaml and the theme files. With manual false it
// first compares stamps with last and reports Changed=false when the files
// are as they were, without reading them.
func loadConfig(path, themeDir, last string, manual bool) tea.Cmd {
	return func() tea.Msg {
		stamp := configStamp(path, themeDir, stampLuaOn(last))
		if !manual && stamp == last {
			return configReloadedMsg{Stamp: stamp}
		}
		st, rep, err := settings.LoadLayered(path)
		if err != nil {
			return configReloadedMsg{Stamp: stamp, Changed: true, Manual: manual, Err: err}
		}
		// lua_config may have flipped with this read: take the stamp the
		// next poll compares against with the new value.
		if on := settings.LuaConfigOn(st); on != stampLuaOn(stamp) {
			stamp = configStamp(path, themeDir, on)
		}
		return configReloadedMsg{Store: st, Themes: themesWithLua(themeDir, rep), Lua: rep, Stamp: stamp, Changed: true, Manual: manual}
	}
}

// liveReloadOn reports whether the TUI polls for config edits.
func (m Model) liveReloadOn() bool { return m.featureOn("live_reload") }

// armConfig starts the config poll when it is not running. Update calls it
// after every message, like armIdle. It waits out the boot splash, needs a
// config path and idleDone (Run's exit signal, which ends the timer), and
// does nothing while live_reload is off: no timer, no stat, no read.
func armConfig(m Model) (Model, tea.Cmd) {
	if m.idleDone == nil || m.configTicking || m.Focus == FocusBoot || m.StorePath == "" || !m.liveReloadOn() {
		return m, nil
	}
	m.configTicking = true
	return m, configTick(m.idleDone)
}

// handleConfigTick runs one poll, or stops the chain when live_reload has
// been turned off (armConfig restarts it when it is turned back on).
func handleConfigTick(m Model) (Model, tea.Cmd) {
	if !m.liveReloadOn() {
		m.configTicking = false
		return m, nil
	}
	return m, loadConfig(m.StorePath, themesDir(), m.configStamp, false)
}

// changedLiveSettings lists the ids of settings that apply live and differ
// between old and cur.
func changedLiveSettings(old, cur *settings.Store) []string {
	var ids []string
	for _, s := range settings.Registry {
		if s.Apply == settings.ApplyLive && !reflect.DeepEqual(old.Get(s.ID), cur.Get(s.ID)) {
			ids = append(ids, s.ID)
		}
	}
	return ids
}

// handleConfigReloaded applies a reload result. A file that does not parse
// keeps the previous settings and says so on the status line. A result that
// was overtaken by another edit (the files changed again while it was being
// read, for instance by the TUI's own save) is dropped; the next poll reads
// the newer files.
func handleConfigReloaded(m Model, msg configReloadedMsg) (Model, tea.Cmd) {
	next := func(m Model) (Model, tea.Cmd) {
		if msg.Manual {
			return m, nil
		}
		if !m.liveReloadOn() {
			m.configTicking = false
			return m, nil
		}
		return m, configTick(m.idleDone)
	}
	if !msg.Changed {
		m.configStamp = msg.Stamp
		return next(m)
	}
	if configStamp(m.StorePath, themesDir(), stampLuaOn(msg.Stamp)) != msg.Stamp {
		return next(m)
	}
	m.configStamp = msg.Stamp
	if msg.Err != nil {
		m.Err = configNotice("config.yaml", msg.Err.Error(), " (kept previous settings)")
		return next(m)
	}
	old := m.Store
	m.Store = msg.Store
	m.Themes = msg.Themes
	m.luaLayer = msg.Lua
	if msg.Lua.Err != nil {
		m.Err = luaNotice(msg.Lua)
	}
	// The theme is re-applied whenever anything changed: its own file may
	// have been edited, not just the setting that names it.
	applyThemeNamed(&m, m.Store.String("theme"))
	for _, id := range changedLiveSettings(old, m.Store) {
		if id != "theme" {
			applyLive(&m, id, m.Store.Get(id))
		}
	}
	return next(m)
}
