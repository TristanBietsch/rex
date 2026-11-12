package tui

import (
	"fmt"
	"os"
	"reflect"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/catalog/theme"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
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

// configStamp summarizes config.yaml and the theme files: it changes when any
// of them is created, edited or removed. It stats paths, not inodes, so an
// editor that saves by rename is seen.
func configStamp(path, themeDir string) string {
	s := "-"
	if fi, err := os.Stat(path); err == nil {
		s = fmt.Sprintf("%d:%d", fi.Size(), fi.ModTime().UnixNano())
	}
	return s + "|" + theme.Stamp(themeDir)
}

// loadConfig re-reads config.yaml and the theme files. With manual false it
// first compares stamps with last and reports Changed=false when the files
// are as they were, without reading them.
func loadConfig(path, themeDir, last string, manual bool) tea.Cmd {
	return func() tea.Msg {
		stamp := configStamp(path, themeDir)
		if !manual && stamp == last {
			return configReloadedMsg{Stamp: stamp}
		}
		st := settings.NewStore()
		if err := st.Load(path); err != nil {
			return configReloadedMsg{Stamp: stamp, Changed: true, Manual: manual, Err: err}
		}
		return configReloadedMsg{Store: st, Themes: theme.LoadDir(themeDir), Stamp: stamp, Changed: true, Manual: manual}
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
	if configStamp(m.StorePath, themesDir()) != msg.Stamp {
		return next(m)
	}
	m.configStamp = msg.Stamp
	if msg.Err != nil {
		m.Err = "config.yaml: " + msg.Err.Error() + " (kept previous settings)"
		return next(m)
	}
	old := m.Store
	m.Store = msg.Store
	m.Themes = msg.Themes
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
