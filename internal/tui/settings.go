package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/tristanbietsch/rex/internal/audio"
	"github.com/tristanbietsch/rex/internal/settings"
)

// SettingsState lives on Model when Focus == FocusSettings.
type SettingsState struct {
	CursorID string
}

func openSettings(m Model) (Model, tea.Cmd) {
	if m.Store == nil {
		m.Store = settings.NewStore()
		m.StorePath = settings.DefaultPath()
		_ = m.Store.Load(m.StorePath)
	}
	st := &SettingsState{}
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
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Foreground(colorFgDim).Render("settings"))
	b.WriteString("\n\n")

	curSection := ""
	for _, s := range settings.Registry {
		if string(s.Section) != curSection {
			if curSection != "" {
				b.WriteString("\n")
			}
			b.WriteString(styleSlug.Render(string(s.Section)) + "\n")
			curSection = string(s.Section)
		}
		cursor := "  "
		if s.ID == m.Settings.CursorID {
			cursor = styleArrow.Render("▸ ")
		}
		label := s.Label
		value := m.Store.String(s.ID)
		row := cursor + fmt.Sprintf("%-26s %s", label, value)
		if s.ID == m.Settings.CursorID {
			row = styleSelected.Render(row)
			if s.Help != "" {
				row += "\n      " + styleDim.Render(s.Help)
			}
		}
		b.WriteString(row + "\n")
	}
	b.WriteString("\n" + styleDim.Render("j/k select · enter toggle/cycle · +/- adjust · r reset · esc close"))
	return b.String()
}
