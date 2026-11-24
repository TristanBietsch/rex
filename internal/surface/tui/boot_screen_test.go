package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/features/audio"
)

func TestBootScreenSettingDefaultsOn(t *testing.T) {
	s, ok := settings.Find("boot_screen")
	require.True(t, ok)
	require.Equal(t, true, s.Default)
	require.Equal(t, settings.SectionAppearance, s.Section)
}

// With boot_screen off the steps run back to back with no step chimes, and
// the board opens on the last step without waiting for the minimum time.
func TestBootHidden_NoChimesAndImmediateHandOff(t *testing.T) {
	ran := 0
	bootSequence = make([]stepFunc, 2)
	for i := range bootSequence {
		bootSequence[i] = func(_ Model) tea.Cmd {
			ran++
			return func() tea.Msg { return nil }
		}
	}
	rec := &recordingAudio{}
	m := Model{Focus: FocusBoot, BootStart: time.Now(), Audio: rec, BootHidden: true, BootMinDone: true}

	mi, cmd := m.appendBootStep(bootStepMsg{Name: "a", Status: stepOK})
	m = mi.(Model)
	require.NotNil(t, cmd, "the next step is scheduled at once")
	require.Equal(t, 1, ran, "the next step is built without the inter-step tick")
	require.Equal(t, FocusBoot, m.Focus)

	mi, _ = m.appendBootStep(bootStepMsg{Name: "b", Status: stepWarn})
	m = mi.(Model)
	require.Equal(t, FocusBoard, m.Focus, "hands off on the last step")
	require.NotContains(t, rec.Events, audio.EventBootOK)
	require.NotContains(t, rec.Events, audio.EventBootWarn)
	require.Contains(t, rec.Events, audio.EventStartup, "the startup chime still plays")
}

// The hidden boot shows one line; a failed step shows the full splash with
// its cause, and still chimes the failure.
func TestBootHidden_ViewAndFailure(t *testing.T) {
	bootSequence = make([]stepFunc, 2)
	for i := range bootSequence {
		bootSequence[i] = func(_ Model) tea.Cmd { return nil }
	}
	rec := &recordingAudio{}
	m := Model{Focus: FocusBoot, BootStart: time.Now(), Audio: rec, BootHidden: true, BootMinDone: true, TermW: 80, TermH: 24}

	view := ansi.Strip(m.View())
	require.Contains(t, view, "starting rex…")
	require.NotContains(t, view, "booting")

	mi, _ := m.appendBootStep(bootStepMsg{Name: "daemon", Status: stepFail, Err: errors.New("no socket")})
	m = mi.(Model)
	view = ansi.Strip(m.View())
	require.Contains(t, view, "boot failed")
	require.Contains(t, view, "no socket")
	require.True(t, strings.Contains(view, "daemon"))
	require.Contains(t, rec.Events, audio.EventBootFail)
}

func TestRenderBootHidden_TinyTerminal(t *testing.T) {
	require.Equal(t, "\n  "+styleDim.Render("starting rex…"), renderBootHidden(0))
	require.Len(t, strings.Split(renderBootHidden(10), "\n"), 10)
}
