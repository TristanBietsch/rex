package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
)

// The theme step used to call applyTheme inside its Cmd goroutine, writing
// the package-level palette and styles while View read them on the event
// loop (spinner ticks keep rendering during boot): a data race. The Cmd now
// only reports the scheme; Update applies it.
func TestStepThemeApply_AppliesOnEventLoop(t *testing.T) {
	prevSeq := bootSequence
	t.Cleanup(func() {
		bootSequence = prevSeq
		applyTheme("default")
	})
	bootSequence = []stepFunc{func(Model) tea.Cmd { return nil }, func(Model) tea.Cmd { return nil }}
	applyTheme("default")

	store := settings.NewStore()
	require.NoError(t, store.Set("color_scheme", "noir"))
	m := Model{Focus: FocusBoot, BootStart: time.Now(), Store: store}

	before := colorFgPrimary
	msg := stepThemeApply(m)()
	require.Equal(t, themeApplyMsg{Scheme: "noir"}, msg)
	require.Equal(t, before, colorFgPrimary, "the step goroutine must not touch the palette")

	out, _ := m.Update(msg)
	m = out.(Model)
	require.Equal(t, palettes["noir"].FgPrimary, colorFgPrimary)
	require.Len(t, m.BootLog, 1)
	require.Equal(t, "theme.apply", m.BootLog[0].Name)
	require.Equal(t, stepOK, m.BootLog[0].Status)
}

func TestStepThemeApply_DefaultSkips(t *testing.T) {
	prevSeq := bootSequence
	t.Cleanup(func() { bootSequence = prevSeq })
	bootSequence = []stepFunc{func(Model) tea.Cmd { return nil }, func(Model) tea.Cmd { return nil }}

	m := Model{Focus: FocusBoot, BootStart: time.Now()}
	out, _ := m.Update(stepThemeApply(m)())
	m = out.(Model)
	require.Len(t, m.BootLog, 1)
	require.Equal(t, stepSkip, m.BootLog[0].Status)
}
