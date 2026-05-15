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
	Store    *settings.Store
	Path     string
	CursorID string
}

func openSettings(m Model) (Model, tea.Cmd) {
	store := settings.NewStore()
	path := settings.DefaultPath()
	if err := store.Load(path); err != nil {
		m.Err = "settings load: " + err.Error()
		return m, nil
	}
	st := &SettingsState{Store: store, Path: path}
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
	if m.Settings != nil {
		// Best-effort save on close.
		_ = m.Settings.Store.Save(m.Settings.Path)
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
		return applySettingsAction(m), nil
	case "r":
		_ = m.Settings.Store.Reset(m.Settings.CursorID)
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

// applySettingsAction toggles bools, cycles enums, no-op for other types in v0.
func applySettingsAction(m Model) Model {
	s, ok := settings.Find(m.Settings.CursorID)
	if !ok || s.ReadOnly {
		return m
	}
	cur := m.Settings.Store.Get(s.ID)
	switch s.Type {
	case settings.TypeBool:
		b, _ := cur.(bool)
		_ = m.Settings.Store.Set(s.ID, !b)
	case settings.TypeEnum:
		curStr := fmt.Sprintf("%v", cur)
		next := s.Options[0]
		for i, opt := range s.Options {
			if opt == curStr {
				next = s.Options[(i+1)%len(s.Options)]
				break
			}
		}
		_ = m.Settings.Store.Set(s.ID, next)
	}
	return m
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
		value := m.Settings.Store.String(s.ID)
		row := cursor + fmt.Sprintf("%-26s %s", label, value)
		if s.ID == m.Settings.CursorID {
			row = styleSelected.Render(row)
			if s.Help != "" {
				row += "\n      " + styleDim.Render(s.Help)
			}
		}
		b.WriteString(row + "\n")
	}
	b.WriteString("\n" + styleDim.Render("j/k select · enter toggle/cycle · r reset · esc close (saves)"))
	return b.String()
}
