package tui

import (
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/catalog/theme"
	"github.com/tristanbietsch/rex/internal/features/audio"
)

// SettingsState lives on Model when Focus == FocusSettings.
type SettingsState struct {
	CursorID string
	// Search keeps the settings whose label, id, section or help contains
	// every word; Searching is whether keys edit it.
	Search    string
	Searching bool
	// HelpExpanded shows the whole help text in the detail pane.
	HelpExpanded bool
	// Edit is the open inline editor of a free-form string setting.
	Edit *settingEdit
	// Tools backs the quick-spawn tool/model/effort pickers so they only
	// offer combinations the daemon will accept.
	Tools []registry.Tool
	// Onboarding lists the tools the wizard leaves out because their binary
	// is not on PATH (Settings → Onboarding).
	Onboarding []onboardRow
	// Pane is the focused pane of the page (fullscreen_pages on).
	Pane pagePane
	// preSearch is CursorID when / opened on the page; esc while typing
	// restores it.
	preSearch string
}

// settingEdit is the inline editor of one free-form string setting. Saving
// goes through Store.Set, so the registry's Normalize is the only validator
// (the CLI and rex setup run the same one).
type settingEdit struct {
	ID  string
	ed  lineEditor
	Err string // why the last save was refused
}

// Text is the draft value.
func (e *settingEdit) Text() string { return e.ed.Text }

// settingFreeForm reports whether enter edits the setting as text. It is
// opt-in (Setting.Free): a string without options is not free by type, since
// the quick-spawn pickers have none in the registry yet cycle through the
// tools registry (and repair their dependents) on enter.
func settingFreeForm(s settings.Setting) bool {
	return s.Free && s.Type == settings.TypeString && !s.ReadOnly
}

// openSettingEdit starts editing the cursor setting with its current value;
// accent_color at its default starts empty: "theme" is not a color to edit.
func openSettingEdit(m Model, s settings.Setting) Model {
	text := m.Store.String(s.ID)
	if s.ID == "accent_color" && text == theme.AccentDefault {
		text = ""
	}
	m.Settings.Edit = &settingEdit{ID: s.ID, ed: lineEditor{Text: text, StripControl: true}}
	return m
}

// settingEditKey drives the inline editor: enter saves through Store.Set
// (a refusal keeps the editor open with the reason), esc cancels, every
// other key edits the text.
func settingEditKey(m Model, k tea.KeyMsg) Model {
	e := m.Settings.Edit
	switch k.Type {
	case tea.KeyEsc:
		m.Settings.Edit = nil
		if e.ID == "accent_color" {
			applyThemeNamed(&m, m.Store.String("theme")) // drop the preview
		}
	case tea.KeyEnter:
		if settingLocked(&m, e.ID) {
			m.Settings.Edit = nil
			return m
		}
		if err := m.Store.Set(e.ID, e.ed.Text); err != nil {
			e.Err = err.Error()
			return m
		}
		applyLive(&m, e.ID, m.Store.Get(e.ID))
		_ = m.Store.Save(m.StorePath)
		reloadDaemonForTrust(m.reloader, m.Socket, e.ID)
		m.Settings.Edit = nil
	default:
		if e.ed.Edit(k) {
			e.Err = ""
			previewAccent(m, e)
		}
	}
	return m
}

// previewAccent shows a draft accent_color at once: a value that parses
// becomes the live accent; anything else keeps the last one. esc or a save
// puts the stored value back in charge (applyThemeNamed).
func previewAccent(m Model, e *settingEdit) {
	if e.ID != "accent_color" {
		return
	}
	if _, err := theme.ParseAccent(e.Text()); err == nil {
		accentChoice = e.Text()
		reapplyTheme(m)
	}
}

func openSettings(m Model) (Model, tea.Cmd) {
	if m.Store == nil {
		store, path, err := settings.LoadDefault()
		if path == "" {
			// No config path (no home directory): nothing could be saved.
			m.Err = "settings: " + err.Error()
			return m, nil
		}
		m.Store, m.StorePath = store, path
	}
	st := &SettingsState{}
	if reg, err := loadToolsStrict(); err == nil {
		st.Tools, st.Onboarding = pickerTools(m, reg)
	} else {
		slog.Warn("tui: settings could not load tool registry; spawn pickers disabled", "err", err)
	}
	if vis := settings.Visible(); len(vis) > 0 {
		st.CursorID = vis[0].ID
	}
	m.Settings = st
	m.Focus = FocusSettings
	if m.Audio != nil {
		m.Audio.Play(audio.EventOpen)
	}
	return m, nil
}

func closeSettings(m Model) Model {
	if m.Store != nil {
		_ = m.Store.Save(m.StorePath)
	}
	m.Settings = nil
	m.Focus = FocusBoard
	if m.Audio != nil {
		m.Audio.Play(audio.EventClose)
	}
	return m
}

func updateSettingsKey(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	m, cmd := settingsKey(m, k)
	// mouse_enabled applies live: capture follows the store on every edit.
	m, mc := syncMouse(m, "setting")
	return m, tea.Batch(cmd, mc)
}

func settingsKey(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	if m.Settings == nil {
		return m, nil
	}
	if m.Settings.Edit != nil {
		return settingEditKey(m, k), nil
	}
	if m.Settings.Searching {
		return settingsSearchKey(m, k), nil
	}
	if pagesOn(m) {
		// The page footer shows m.Err in place of the hints: a refusal
		// lasts until the next key.
		m.Err = ""
		if next, ok := settingsPageKey(m, k); ok {
			return next, nil
		}
	}
	if settingsValueKeys[k.String()] && m.Settings.Search != "" && !settingsListed(m.Settings, m.Settings.CursorID) {
		return m, nil // a kept search with no hits shows no row to change
	}
	switch k.String() {
	case "esc":
		if m.Settings.Search != "" {
			m.Settings.Search = ""
			return m, nil
		}
		return closeSettings(m), nil
	case "q":
		if pagesOn(m) {
			return closeSettings(m), nil
		}
		return m, nil
	case "/":
		m.Settings.Searching = true
		return m, nil
	case "?":
		m.Settings.HelpExpanded = !m.Settings.HelpExpanded
		return m, nil
	case "tab":
		m.Settings.CursorID = settingsSectionJump(m.Settings, +1)
		return m, nil
	case "shift+tab":
		m.Settings.CursorID = settingsSectionJump(m.Settings, -1)
		return m, nil
	case "j", "down":
		m.Settings.CursorID = settingsStep(m, +1)
		return m, nil
	case "k", "up":
		m.Settings.CursorID = settingsStep(m, -1)
		return m, nil
	case "e":
		// A free-form row config.lua does not set: edit the value here.
		if s, ok := settings.Find(m.Settings.CursorID); ok && settingFreeForm(s) && !settingLocked(&m, s.ID) {
			return openSettingEdit(m, s), nil
		}
		return openLockedInEditor(m, m.Settings.CursorID)
	case "#":
		// Typing a hex color on the accent row starts the editor with it.
		if m.Settings.CursorID == "accent_color" && !settingLocked(&m, "accent_color") {
			s, _ := settings.Find("accent_color")
			m = openSettingEdit(m, s)
			m.Settings.Edit.ed = lineEditor{Text: "#", StripControl: true}
		}
		return m, nil
	case "enter", " ":
		if settingLocked(&m, m.Settings.CursorID) {
			return m, nil
		}
		if s, ok := settings.Find(m.Settings.CursorID); ok && settingFreeForm(s) {
			return openSettingEdit(m, s), nil
		}
		return applySettingsAction(m, 0), nil
	case "+", "=", "l", "right":
		return applySettingsAction(m, +1), nil
	case "-", "_", "h", "left":
		return applySettingsAction(m, -1), nil
	case "R":
		// Reload every layer, like :reload; the page stays open.
		return runReload(m, cmdCall{Verb: "reload"})
	case "r":
		s, ok := settings.Find(m.Settings.CursorID)
		if ok && !settingLocked(&m, s.ID) {
			_ = m.Store.Reset(m.Settings.CursorID)
			applyLive(&m, s.ID, m.Store.Get(s.ID))
			_ = m.Store.Save(m.StorePath)
			reloadDaemonForTrust(m.reloader, m.Socket, s.ID)
		}
		return m, nil
	}
	return m, nil
}

// settingsValueKeys change, reset or edit the cursor row.
var settingsValueKeys = map[string]bool{
	"enter": true, " ": true, "+": true, "=": true, "l": true, "right": true,
	"-": true, "_": true, "h": true, "left": true, "r": true, "e": true,
}

// settingsSearchKey edits the settings search. The query is bounded (see
// compileQuery) and stripped of control runes.
func settingsSearchKey(m Model, k tea.KeyMsg) Model {
	st := m.Settings
	switch k.Type {
	case tea.KeyEsc:
		st.Search, st.Searching = "", false
		if pagesOn(m) && settingsListed(st, st.preSearch) {
			st.CursorID = st.preSearch
		}
	case tea.KeyEnter:
		st.Searching = false
	case tea.KeyBackspace:
		if r := []rune(st.Search); len(r) > 0 {
			st.Search = string(r[:len(r)-1])
		}
	case tea.KeyCtrlU:
		st.Search = ""
	case tea.KeyRunes, tea.KeySpace:
		add := strings.Map(func(r rune) rune {
			if r < ' ' || r == 0x7f {
				return -1
			}
			return r
		}, string(k.Runes))
		if k.Type == tea.KeySpace {
			add = " "
		}
		if len([]rune(st.Search+add)) <= maxSearchRunes {
			st.Search += add
		}
	default:
	}
	st.CursorID = settingsKeepCursor(st)
	return m
}

// settingsList is the settings the page lists: the visible ones that match
// the search, in registry order.
func settingsList(st *SettingsState) []settings.Setting {
	vis := slices.Clone(settings.Visible())
	for _, r := range st.Onboarding {
		vis = append(vis, r.setting())
	}
	terms := compileQuery(st.Search)
	if len(terms) == 0 {
		return vis
	}
	var out []settings.Setting
	for _, s := range vis {
		hay := strings.ToLower(s.Label + " " + s.ID + " " + string(s.Section) + " " + s.Help)
		ok := true
		for _, t := range terms {
			if !strings.Contains(hay, t) {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, s)
		}
	}
	return out
}

// settingsKeepCursor is the cursor id after the list changed: the same row
// when it is still listed, else the first one.
func settingsKeepCursor(st *SettingsState) string {
	list := settingsList(st)
	for _, s := range list {
		if s.ID == st.CursorID {
			return st.CursorID
		}
	}
	if len(list) > 0 {
		return list[0].ID
	}
	return st.CursorID
}

// settingsAfter returns the id delta rows after the cursor in the listed
// settings, stopping at the ends.
func settingsAfter(st *SettingsState, delta int) string {
	return settingsAfterIn(settingsList(st), st.CursorID, delta)
}

// settingsAfterIn returns the id delta rows after cur in list, stopping at
// the ends; cur when list is empty.
func settingsAfterIn(list []settings.Setting, cur string, delta int) string {
	if len(list) == 0 {
		return cur
	}
	idx := 0
	for i, s := range list {
		if s.ID == cur {
			idx = i
			break
		}
	}
	idx = min(max(idx+delta, 0), len(list)-1)
	return list[idx].ID
}

// settingsStep is the cursor id delta rows on: within the page's list
// (the cursor's section, or the search hits) on a page, else the overlay's
// whole list.
func settingsStep(m Model, delta int) string {
	if pagesOn(m) {
		return settingsAfterIn(settingsPageList(m), m.Settings.CursorID, delta)
	}
	return settingsAfter(m.Settings, delta)
}

// settingsListed reports whether id is in the listed settings.
func settingsListed(st *SettingsState, id string) bool {
	for _, s := range settingsList(st) {
		if s.ID == id {
			return true
		}
	}
	return false
}

// settingsSections is the sections of the unfiltered list, in order of
// first appearance (Onboarding included when it has rows).
func settingsSections(st *SettingsState) []settings.Section {
	var out []settings.Section
	for _, s := range settingsList(&SettingsState{Onboarding: st.Onboarding}) {
		if !slices.Contains(out, s.Section) {
			out = append(out, s.Section)
		}
	}
	return out
}

// settingsSectionOf is the section of the cursor row; the first section
// when the cursor is not listed.
func settingsSectionOf(st *SettingsState) settings.Section {
	for _, s := range settingsList(&SettingsState{Onboarding: st.Onboarding}) {
		if s.ID == st.CursorID {
			return s.Section
		}
	}
	if secs := settingsSections(st); len(secs) > 0 {
		return secs[0]
	}
	return ""
}

// settingsPageList is the rows the page lists: the search hits across
// every section while a search is set, else the cursor section's rows.
func settingsPageList(m Model) []settings.Setting {
	st := m.Settings
	if st.Search != "" {
		return settingsList(st)
	}
	sec := settingsSectionOf(st)
	var out []settings.Setting
	for _, s := range settingsList(st) {
		if s.Section == sec {
			out = append(out, s)
		}
	}
	return out
}

// settingsGoSection is the id of the first row of section idx (clamped to
// the sections).
func settingsGoSection(st *SettingsState, idx int) string {
	secs := settingsSections(st)
	if len(secs) == 0 {
		return st.CursorID
	}
	sec := secs[min(max(idx, 0), len(secs)-1)]
	for _, s := range settingsList(&SettingsState{Onboarding: st.Onboarding}) {
		if s.Section == sec {
			return s.ID
		}
	}
	return st.CursorID
}

// settingsSectionIndex is the index of the cursor's section in
// settingsSections.
func settingsSectionIndex(st *SettingsState) int {
	return max(slices.Index(settingsSections(st), settingsSectionOf(st)), 0)
}

// settingsFirstHit is the id of the first listed row of sec: its first row,
// or its first search hit while a search is set. ok false when sec lists
// nothing.
func settingsFirstHit(st *SettingsState, sec settings.Section) (string, bool) {
	for _, s := range settingsList(st) {
		if s.Section == sec {
			return s.ID, true
		}
	}
	return "", false
}

// settingsHitSection is the first hit of the nearest section in direction
// dir (±1) that has hits under the kept search, skipping sections without
// any. wrap cycles past the ends; otherwise it clamps. The cursor id when no
// such section exists.
func settingsHitSection(st *SettingsState, dir int, wrap bool) string {
	secs := settingsSections(st)
	n, cur := len(secs), settingsSectionIndex(st)
	for i := 1; i < n; i++ {
		idx := cur + dir*i
		if wrap {
			idx = (idx%n + n) % n
		} else if idx < 0 || idx >= n {
			break
		}
		if id, ok := settingsFirstHit(st, secs[idx]); ok {
			return id
		}
	}
	return st.CursorID
}

// settingsSectionMove is the cursor after a left-pane step of dir (±1)
// through the sections, clamped: the next section's first row, or under a
// kept search the first hit of the next section that has hits.
func settingsSectionMove(st *SettingsState, dir int) string {
	if st.Search != "" {
		return settingsHitSection(st, dir, false)
	}
	return settingsGoSection(st, settingsSectionIndex(st)+dir)
}

// settingsModifiedIn counts the rows of sec whose value differs from the
// default.
func settingsModifiedIn(m Model, sec settings.Section) int {
	n := 0
	for _, s := range settingsList(&SettingsState{Onboarding: m.Settings.Onboarding}) {
		if s.Section == sec && settingModified(m, s) {
			n++
		}
	}
	return n
}

// settingsPageKey handles the keys the page adds or changes (fullscreen_pages
// on, no search or edit open). ok false hands the key to the shared rows
// keys in settingsKey.
func settingsPageKey(m Model, k tea.KeyMsg) (Model, bool) {
	st := m.Settings
	key := k.String()
	switch key {
	case "tab", "shift+tab":
		if !pageNarrow(m.Width) {
			if st.Pane == paneRows {
				st.Pane = paneSections
			} else {
				st.Pane = paneRows
			}
		}
		return m, true
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		secs := settingsSections(st)
		n := int(key[0] - '1')
		switch {
		case n >= len(secs):
		case st.Search != "":
			// A kept search: the section's first hit; no hits, no move.
			if id, ok := settingsFirstHit(st, secs[n]); ok {
				st.CursorID = id
			}
		default:
			st.CursorID = settingsGoSection(st, n)
		}
		return m, true
	case "[", "]":
		n := len(settingsSections(st))
		if n == 0 {
			return m, true
		}
		dir := 1
		if key == "[" {
			dir = -1
		}
		if st.Search != "" {
			st.CursorID = settingsHitSection(st, dir, true)
		} else {
			st.CursorID = settingsGoSection(st, (settingsSectionIndex(st)+dir+n)%n)
		}
		return m, true
	case "/":
		st.Searching, st.Pane, st.preSearch = true, paneRows, st.CursorID
		return m, true
	case "esc", "q":
		return m, false // settingsKey: clear a kept search, else close
	}
	// A narrow page hides the section pane (and tab can't reach it), so
	// a left-pane focus left over from a wider terminal acts as rows.
	if st.Pane == paneSections && !pageNarrow(m.Width) {
		switch key {
		case "j", "down":
			st.CursorID = settingsSectionMove(st, +1)
		case "k", "up":
			st.CursorID = settingsSectionMove(st, -1)
		case "enter", "l", "right":
			st.Pane = paneRows
		}
		return m, true
	}
	if key == "h" {
		// Only h is a no-op on a fixed row; left, - and _ still change values.
		// A row config.lua locks falls through, so h explains the lock.
		if s, ok := settings.Find(st.CursorID); !ok || !settingAdjustable(m, s) && settingLockedAt(m, s.ID) == "" {
			return m, true
		}
	}
	return m, false
}

// settingsSectionJump moves the cursor to the first row of the next section
// (dir +1, wrapping) or, going back, to the first row of the current section,
// or of the previous one when it is already there.
func settingsSectionJump(st *SettingsState, dir int) string {
	list := settingsList(st)
	if len(list) == 0 {
		return st.CursorID
	}
	idx := 0
	for i, s := range list {
		if s.ID == st.CursorID {
			idx = i
			break
		}
	}
	// starts are the indexes where a section begins.
	var starts []int
	for i, s := range list {
		if i == 0 || s.Section != list[i-1].Section {
			starts = append(starts, i)
		}
	}
	cur := 0
	for n, at := range starts {
		if at <= idx {
			cur = n
		}
	}
	switch {
	case dir > 0:
		cur = (cur + 1) % len(starts)
	case starts[cur] < idx:
		// back inside a section: its own first row
	default:
		cur = (cur - 1 + len(starts)) % len(starts)
	}
	return list[starts[cur]].ID
}

// applySettingsAction mutates the cursor row.
//
//	step =  0 → toggle bool / cycle enum / no-op for float/int (enter)
//	step = +1 → cycle enum forward, step bool to true, increment float/int
//	step = -1 → cycle enum back, step bool to false, decrement float/int
func applySettingsAction(m Model, step int) Model {
	s, ok := settings.Find(m.Settings.CursorID)
	if !ok || s.ReadOnly || settingLocked(&m, s.ID) {
		return m
	}
	cur := m.Store.Get(s.ID)
	if opts, ok := dynamicOptions(m, s.ID); ok {
		if len(opts) == 0 {
			return m
		}
		next := cycleOption(opts, fmt.Sprintf("%v", cur), step)
		if s.ID == "theme" {
			return setThemeSetting(m, next)
		}
		return setSpawnDefault(m, s.ID, next)
	}
	var next any
	switch s.Type {
	case settings.TypeBool:
		b, _ := cur.(bool)
		switch step {
		case +1:
			next = true
		case -1:
			next = false
		default:
			next = !b
		}
	case settings.TypeEnum:
		curStr := fmt.Sprintf("%v", cur)
		if len(s.Options) == 0 {
			return m
		}
		idx := 0
		for i, opt := range s.Options {
			if opt == curStr {
				idx = i
				break
			}
		}
		switch step {
		case -1:
			idx = (idx - 1 + len(s.Options)) % len(s.Options)
		default:
			idx = (idx + 1) % len(s.Options)
		}
		next = s.Options[idx]
	case settings.TypeString:
		if len(s.Options) == 0 {
			return m
		}
		curStr := fmt.Sprintf("%v", cur)
		idx := -1
		for i, opt := range s.Options {
			if opt == curStr {
				idx = i
				break
			}
		}
		switch step {
		case -1:
			if idx < 0 {
				idx = len(s.Options) - 1
			} else {
				idx = (idx - 1 + len(s.Options)) % len(s.Options)
			}
		default:
			if idx < 0 {
				idx = 0
			} else {
				idx = (idx + 1) % len(s.Options)
			}
		}
		next = s.Options[idx]
	case settings.TypeFloat:
		f, _ := cur.(float64)
		const fStep = 0.05
		switch step {
		case +1:
			f += fStep
		case -1:
			f -= fStep
		default:
			return m
		}
		if s.Max > s.Min {
			if f < s.Min {
				f = s.Min
			}
			if f > s.Max {
				f = s.Max
			}
		}
		next = f
	case settings.TypeInt:
		i, _ := cur.(int)
		switch step {
		case +1:
			i++
		case -1:
			i--
		default:
			return m
		}
		if s.Min != 0 || s.Max != 0 {
			if float64(i) < s.Min {
				i = int(s.Min)
			}
			if float64(i) > s.Max {
				i = int(s.Max)
			}
		}
		next = i
	default:
		return m
	}
	if err := m.Store.Set(s.ID, next); err != nil {
		return m
	}
	applyLive(&m, s.ID, m.Store.Get(s.ID))
	_ = m.Store.Save(m.StorePath)
	reloadDaemonForTrust(m.reloader, m.Socket, s.ID)
	return m
}

// cycleOption returns the option step positions from cur (wrapping); an
// unknown cur starts at the first (forward) or last (backward) option.
func cycleOption(opts []string, cur string, step int) string {
	idx := -1
	for i, o := range opts {
		if o == cur {
			idx = i
			break
		}
	}
	switch {
	case idx < 0 && step < 0:
		return opts[len(opts)-1]
	case idx < 0:
		return opts[0]
	case step < 0:
		return opts[(idx-1+len(opts))%len(opts)]
	default:
		return opts[(idx+1)%len(opts)]
	}
}

// dynamicOptions returns the choices that are computed rather than listed in
// the registry: the themes (built-ins plus the user's files) and the
// registry-backed quick-spawn settings. ok is false for every other setting.
func dynamicOptions(m Model, id string) ([]string, bool) {
	switch id {
	case "theme":
		return themeOptions(m), true
	case "default_spawn_tool", "default_spawn_model", "default_spawn_effort":
	default:
		return nil, false
	}
	if m.Settings == nil || len(m.Settings.Tools) == 0 {
		return nil, true
	}
	tool, model, _ := currentSpawnChoice(m)
	var out []string
	switch id {
	case "default_spawn_tool":
		for _, t := range m.Settings.Tools {
			out = append(out, t.ID)
		}
	case "default_spawn_model":
		for _, md := range tool.Models {
			out = append(out, md.ID)
		}
	case "default_spawn_effort":
		if model.Effort != nil {
			out = append(out, model.Effort.Options...)
		}
	}
	return out, true
}

// currentSpawnChoice resolves the stored quick-spawn tool/model against the
// registry, falling back to the first tool / first model.
func currentSpawnChoice(m Model) (registry.Tool, registry.Model, bool) {
	toolID, _ := m.Store.Get("default_spawn_tool").(string)
	modelID, _ := m.Store.Get("default_spawn_model").(string)
	tools := m.Settings.Tools
	tool := tools[0]
	found := false
	for _, t := range tools {
		if t.ID == toolID {
			tool, found = t, true
			break
		}
	}
	model := tool.Models[0]
	for _, md := range tool.Models {
		if md.ID == modelID {
			model = md
			break
		}
	}
	return tool, model, found
}

// setSpawnDefault stores one quick-spawn setting and repairs the dependent
// ones: a new tool resets model + effort, a new model resets an effort the
// model doesn't offer.
func setSpawnDefault(m Model, id, value string) Model {
	if settingLocked(&m, id) {
		return m
	}
	// Run the cascade on a copy first: when it would change a setting
	// config.lua locks, refuse the whole change.
	trial := m
	trial.Store = m.Store.WithDefaults()
	if err := spawnCascade(trial, id, value); err != nil {
		if locked, ok := errors.AsType[*settings.ErrManagedByLua](err); ok {
			m.Err = lockMessage(locked.ID, locked.At)
		} else {
			m.Err = "settings: " + err.Error()
		}
		return m
	}
	_ = spawnCascade(m, id, value)
	slog.Info("tui: quick-spawn default changed", "setting", id, "value", value,
		"model", m.Store.Get("default_spawn_model"), "effort", m.Store.Get("default_spawn_effort"))
	_ = m.Store.Save(m.StorePath)
	return m
}

// spawnCascade sets id to value in m.Store and repairs the dependent
// quick-spawn settings. It stops when id is refused or a repair would change
// a setting config.lua sets.
func spawnCascade(m Model, id, value string) error {
	if err := m.Store.Set(id, value); err != nil {
		return err
	}
	_, model, _ := currentSpawnChoice(m)
	set := func(id string, v any) error {
		if fmt.Sprint(m.Store.Get(id)) == fmt.Sprint(v) {
			return nil // unchanged: a locked value that already fits stays
		}
		// Only a lock stops the cascade; other refusals of a repair were
		// always ignored.
		if err := m.Store.Set(id, v); errors.As(err, new(*settings.ErrManagedByLua)) {
			return err
		}
		return nil
	}
	if id == "default_spawn_tool" {
		if err := set("default_spawn_model", model.ID); err != nil {
			return err
		}
	}
	effort, _ := m.Store.Get("default_spawn_effort").(string)
	switch {
	case model.Effort == nil:
		return set("default_spawn_effort", "")
	case !containsString(model.Effort.Options, effort):
		return set("default_spawn_effort", model.Effort.Default)
	}
	return nil
}

func containsString(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// windowLines keeps at most max lines of lines, positioned so focus stays
// visible, with ↑/↓ markers where content is cut.
func windowLines(lines []string, focus, max int) []string {
	if max <= 0 || len(lines) <= max {
		return lines
	}
	if max < 3 {
		max = 3
	}
	start := focus - max/2
	if start < 0 {
		start = 0
	}
	if start+max > len(lines) {
		start = len(lines) - max
	}
	out := append([]string{}, lines[start:start+max]...)
	if start > 0 {
		out[0] = styleDim.Render("  ↑ more")
	}
	if start+max < len(lines) {
		out[len(out)-1] = styleDim.Render("  ↓ more")
	}
	return out
}

// applyLive performs the side effect for a setting change. Renderers read the
// store live, so display-only settings (spinner, glyph, etc.) don't need a
// hook here — they're picked up on the next View().
func applyLive(m *Model, id string, value any) {
	switch id {
	case "theme":
		if name, ok := value.(string); ok {
			applyThemeNamed(m, name)
			noteAccent(m)
		}
	case "accent_color":
		applyThemeNamed(m, m.Store.String("theme"))
		noteAccent(m)
	case "sound_enabled":
		if m.Audio != nil {
			if b, ok := value.(bool); ok {
				ss, _ := m.Store.Get("soundset").(string)
				m.Audio.SetEnabled(b && ss != "off")
			}
		}
	case "soundset":
		if m.Audio != nil {
			if s, ok := value.(string); ok {
				se, _ := m.Store.Get("sound_enabled").(bool)
				m.Audio.SetEnabled(se && s != "off")
				if s != "off" {
					m.Audio.SetSoundset(s)
				}
			}
		}
	case "boot_voice":
		if m.Audio != nil {
			if b, ok := value.(bool); ok {
				m.Audio.SetBootVoice(b)
			}
		}
	case "master_volume":
		if m.Audio != nil {
			m.Audio.SetVolume(settings.VolumeGain(value))
		}
	case "extra_agents":
		*m = refreshPickers(*m) // the open wizard and pickers drop or regain qwen, cursor, grok
	case "board_hidden_sections":
		// The same reader as startup, so board_filter off still ignores it.
		*m = applyHidden(*m, hiddenSectionsFrom(m.Store))
	case "max_concurrent_sessions":
		if m.Client != nil {
			if n, ok := value.(int); ok {
				if err := m.Client.SetMaxConcurrent(n); err != nil {
					m.Err = "set max concurrent: " + err.Error()
				}
			}
		}
	}
}

// settingsHints are the footer key hints, in display order.
var settingsHints = []string{"↑↓ select", "←→ change", "enter toggle", "/ search", "tab section", "r reset", "R reload", "? help", "esc close"}

// settingsLockedHints replace settingsHints on a row config.lua sets.
var settingsLockedHints = []string{"↑↓ select", "e open config.lua", "/ search", "tab section", "? help", "esc close"}

// settingsEditHints are the footer hints while the inline editor is open.
var settingsEditHints = []string{"enter save", "esc cancel", "ctrl+u clear"}

// wrapHints packs hints (3-space separated) into lines of at most width cells.
func wrapHints(hints []string, width int) []string {
	const sep = "   "
	var lines []string
	cur := ""
	for _, h := range hints {
		switch {
		case cur == "":
			cur = h
		case ansi.StringWidth(cur)+len(sep)+ansi.StringWidth(h) <= width:
			cur += sep + h
		default:
			lines = append(lines, cur)
			cur = h
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	for i, l := range lines {
		lines[i] = styleDim.Render(ansi.Truncate(l, width, "…"))
	}
	return lines
}

// settingAdjustable reports whether left/right changes the setting.
func settingAdjustable(m Model, s settings.Setting) bool {
	if s.ReadOnly || settingLockedAt(m, s.ID) != "" {
		return false
	}
	if _, ok := dynamicOptions(m, s.ID); ok {
		return true
	}
	switch s.Type {
	case settings.TypeEnum, settings.TypeFloat, settings.TypeInt:
		return true
	case settings.TypeString:
		return len(s.Options) > 0
	case settings.TypeBool:
		return false
	}
	return false
}

// settingValueText is the plain (unstyled) display value.
func settingValueText(m Model, s settings.Setting) string {
	if _, ok := onboardRowOf(m.Settings, s.ID); ok {
		return "not on PATH"
	}
	v := m.Store.Display(s.ID)
	if s.Type == settings.TypeBool {
		if b, _ := m.Store.Get(s.ID).(bool); b {
			return "on"
		}
		return "off"
	}
	if v == "" {
		return "—"
	}
	return strings.ReplaceAll(v, "\n", " ")
}

// padCells right-pads plain/styled s with spaces to exactly w cells.
func padCells(s string, w int) string {
	if d := w - ansi.StringWidth(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}
