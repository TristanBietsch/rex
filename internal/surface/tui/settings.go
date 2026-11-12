package tui

import (
	"fmt"
	"log/slog"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/features/audio"
)

// SettingsState lives on Model when Focus == FocusSettings.
type SettingsState struct {
	CursorID string
	// Tools backs the quick-spawn tool/model/effort pickers so they only
	// offer combinations the daemon will accept.
	Tools []registry.Tool
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
		st.Tools = visibleTools(reg.Tools)
	} else {
		slog.Warn("tui: settings could not load tool registry; spawn pickers disabled", "err", err)
	}
	if len(settings.Registry) > 0 {
		st.CursorID = settings.Registry[0].ID
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
	switch k.String() {
	case "esc":
		return closeSettings(m), nil
	case "j", "down":
		m.Settings.CursorID = settingsAfter(m.Settings.CursorID, +1)
		return m, nil
	case "k", "up":
		m.Settings.CursorID = settingsAfter(m.Settings.CursorID, -1)
		return m, nil
	case "enter", " ":
		return applySettingsAction(m, 0), nil
	case "+", "=", "l", "right":
		return applySettingsAction(m, +1), nil
	case "-", "_", "h", "left":
		return applySettingsAction(m, -1), nil
	case "r":
		s, ok := settings.Find(m.Settings.CursorID)
		if ok {
			_ = m.Store.Reset(m.Settings.CursorID)
			applyLive(&m, s.ID, m.Store.Get(s.ID))
			_ = m.Store.Save(m.StorePath)
		}
		return m, nil
	}
	return m, nil
}

// settingsAfter returns the id N positions after current in the visible registry order.
func settingsAfter(current string, delta int) string {
	if len(settings.Registry) == 0 {
		return ""
	}
	idx := 0
	for i, s := range settings.Registry {
		if s.ID == current {
			idx = i
			break
		}
	}
	idx += delta
	if idx < 0 {
		idx = 0
	}
	if idx >= len(settings.Registry) {
		idx = len(settings.Registry) - 1
	}
	return settings.Registry[idx].ID
}

// applySettingsAction mutates the cursor row.
//
//	step =  0 → toggle bool / cycle enum / no-op for float/int (enter)
//	step = +1 → cycle enum forward, step bool to true, increment float/int
//	step = -1 → cycle enum back, step bool to false, decrement float/int
func applySettingsAction(m Model, step int) Model {
	s, ok := settings.Find(m.Settings.CursorID)
	if !ok || s.ReadOnly {
		return m
	}
	cur := m.Store.Get(s.ID)
	if opts, ok := spawnPickerOptions(m, s.ID); ok {
		if len(opts) == 0 {
			return m
		}
		return setSpawnDefault(m, s.ID, cycleOption(opts, fmt.Sprintf("%v", cur), step))
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

// spawnPickerOptions returns the registry-backed choices for the quick-spawn
// settings; ok is false for every other setting.
func spawnPickerOptions(m Model, id string) ([]string, bool) {
	switch id {
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
	if err := m.Store.Set(id, value); err != nil {
		m.Err = "settings: " + err.Error()
		return m
	}
	_, model, _ := currentSpawnChoice(m)
	if id == "default_spawn_tool" {
		_ = m.Store.Set("default_spawn_model", model.ID)
	}
	effort, _ := m.Store.Get("default_spawn_effort").(string)
	switch {
	case model.Effort == nil:
		_ = m.Store.Set("default_spawn_effort", "")
	case !containsString(model.Effort.Options, effort):
		_ = m.Store.Set("default_spawn_effort", model.Effort.Default)
	}
	slog.Info("tui: quick-spawn default changed", "setting", id, "value", value,
		"model", m.Store.Get("default_spawn_model"), "effort", m.Store.Get("default_spawn_effort"))
	_ = m.Store.Save(m.StorePath)
	return m
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
	case "color_scheme":
		if name, ok := value.(string); ok {
			applyTheme(name)
		}
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
			if v, ok := value.(float64); ok {
				m.Audio.SetVolume(v)
			}
		}
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
var settingsHints = []string{"↑↓ select", "←→ change", "enter toggle", "r reset", "esc close"}

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
	if s.ReadOnly {
		return false
	}
	if _, ok := spawnPickerOptions(m, s.ID); ok {
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
	v := m.Store.String(s.ID)
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

// renderSettings returns the finished settings box for a termW-wide terminal.
func renderSettings(m Model, termW int) string {
	if m.Settings == nil || m.Store == nil {
		return ""
	}
	if termW <= 0 {
		termW = 100
	}

	var (
		width, padH int
		bordered    = termW >= 40
	)
	switch {
	case termW < 40:
		width, padH = termW, 0
	case termW < 60:
		width, padH = termW-2, 1
	default:
		width, padH = termW-8, 3
		if width < 56 {
			width = 56
		}
		if width > 88 {
			width = 88
		}
	}
	inner := width
	if bordered {
		inner = width - 2 - 2*padH
	}
	labelW := 30
	if lim := inner * 40 / 100; lim < labelW {
		labelW = lim
	}
	if termW < 60 {
		labelW = inner / 2
	}
	ownLine := inner < 40
	selStyle := styleSelected
	selBold := styleSelected.Bold(true)

	var lines []string
	focus := 0
	curSection := ""
	cursorIdx, total := 0, len(settings.Registry)
	var sel settings.Setting
	for i, s := range settings.Registry {
		if string(s.Section) != curSection {
			if curSection != "" {
				lines = append(lines, "")
			}
			lines = append(lines, sectionHeader(string(s.Section), inner))
			curSection = string(s.Section)
		}
		isSel := s.ID == m.Settings.CursorID
		adjustable := settingAdjustable(m, s)
		val := settingValueText(m, s)
		empty := val == "—"
		label := ansi.Truncate(s.Label, labelW, "…")

		valAvail := inner - 2 - labelW - 1
		if ownLine {
			valAvail = inner - 4
		}
		arrows := isSel && adjustable && !empty
		if arrows {
			valAvail -= 4
		}
		if valAvail < 1 {
			valAvail = 1
		}
		val = ansi.Truncate(val, valAvail, "…")
		valCell := val
		if arrows {
			valCell = "◂ " + val + " ▸"
		}

		if !isSel {
			vs := styleDim
			if !s.ReadOnly && !empty {
				vs = stylePrimary
			}
			if ownLine {
				lines = append(lines, "  "+styleDim.Render(label), "    "+vs.Render(valCell))
			} else {
				lines = append(lines, "  "+styleDim.Render(padCells(label, labelW))+" "+vs.Render(valCell))
			}
			continue
		}

		cursorIdx = i + 1
		sel = s
		focus = len(lines)
		marker := styleArrow.Render("▸ ")
		if ownLine {
			first := marker + selBold.Render(label)
			lines = append(lines, first+selStyle.Render(strings.Repeat(" ", max(0, inner-ansi.StringWidth(first)))))
			vs := selStyle
			if s.ReadOnly || empty {
				vs = selStyle.Foreground(colorFgDim)
			}
			second := selStyle.Render("    ") + vs.Render(valCell)
			lines = append(lines, second+selStyle.Render(strings.Repeat(" ", max(0, inner-ansi.StringWidth(second)))))
			continue
		}
		vs := selStyle
		if s.ReadOnly || empty {
			vs = selStyle.Foreground(colorFgDim)
		}
		row := marker + selBold.Render(padCells(label, labelW)) + selStyle.Render(" ") + vs.Render(valCell)
		lines = append(lines, row+selStyle.Render(strings.Repeat(" ", max(0, inner-ansi.StringWidth(row)))))
	}

	// Header: SETTINGS left, "n of N" right. A bordered panel carries both
	// in its top edge; the borderless narrow layout keeps a header row.
	posText := fmt.Sprintf("%d of %d", cursorIdx, total)
	var header string
	if !bordered {
		title := styleBold.Render("SETTINGS")
		pos := styleDim.Render(posText)
		gap := inner - ansi.StringWidth(title) - ansi.StringWidth(pos)
		header = title + strings.Repeat(" ", max(1, gap)) + pos
		if gap < 1 {
			header = ansi.Truncate(title, inner, "")
		}
	}

	// Fixed 2-line help area so the layout never jumps.
	help := []string{"", ""}
	if sel.Help != "" {
		wrapped := strings.Split(ansi.Wordwrap(sel.Help, inner, ""), "\n")
		for i := 0; i < 2 && i < len(wrapped); i++ {
			help[i] = wrapped[i]
		}
		if len(wrapped) > 2 {
			help[1] = ansi.Truncate(help[1]+"…", inner, "…")
		}
	}
	for i, h := range help {
		help[i] = styleDim.Render(ansi.Truncate(h, inner, "…"))
	}
	footer := wrapHints(settingsHints, inner)

	// Non-body rows: blank, help(2), blank, footer, plus two more: the
	// borderless layout's header and blank, or a bordered panel's two border
	// rows (its title sits in the top one).
	chrome := 1 + 2 + 1 + len(footer) + 2
	maxRows := 0
	if m.Height > 0 {
		maxRows = m.Height - chrome
		if maxRows < 3 {
			maxRows = 3
		}
	}

	var out []string
	if !bordered {
		out = append(out, header, "")
	}
	out = append(out, windowLines(lines, focus, maxRows)...)
	out = append(out, "")
	out = append(out, help...)
	out = append(out, "")
	out = append(out, footer...)
	content := strings.Join(out, "\n")

	if !bordered {
		return content
	}
	box, _ := panelLines("SETTINGS", posText, strings.Split(content, "\n"), padH, 0, width)
	return strings.Join(box, "\n")
}
