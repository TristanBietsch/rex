package tui

import (
	"fmt"
	"log/slog"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

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
		m.Store = settings.NewStore()
		m.StorePath = settings.DefaultPath()
		_ = m.Store.Load(m.StorePath)
	}
	st := &SettingsState{}
	if reg, err := registry.Load(toolsConfigPath()); err == nil {
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

// overlayChrome is the border + padding centerOverlay adds around content.
const overlayChrome = 4

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

func renderSettings(m Model) string {
	if m.Settings == nil {
		return ""
	}
	var lines []string
	focus := 0
	curSection := ""
	for _, s := range settings.Registry {
		if string(s.Section) != curSection {
			if curSection != "" {
				lines = append(lines, "")
			}
			lines = append(lines, styleSlug.Render(string(s.Section)))
			curSection = string(s.Section)
		}
		cursor := "  "
		if s.ID == m.Settings.CursorID {
			cursor = styleArrow.Render("▸ ")
		}
		label := s.Label
		value := m.Store.String(s.ID)
		if value == "" {
			value = styleDim.Render("—")
		}
		row := cursor + fmt.Sprintf("%-26s %s", label, value)
		if s.ID == m.Settings.CursorID {
			focus = len(lines)
			lines = append(lines, styleSelected.Render(row))
			if s.Help != "" {
				lines = append(lines, "      "+styleDim.Render(s.Help))
			}
			continue
		}
		lines = append(lines, row)
	}

	// Title (2 lines) + footer (2 lines) + overlay border/padding must fit.
	maxRows := 0
	if m.Height > 0 {
		maxRows = m.Height - overlayChrome - 4
	}
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Foreground(colorFgDim).Render("settings"))
	b.WriteString("\n\n")
	b.WriteString(strings.Join(windowLines(lines, focus, maxRows), "\n"))
	b.WriteString("\n\n" + styleDim.Render("j/k select · enter toggle/cycle · +/- adjust · r reset · esc close"))
	return b.String()
}
