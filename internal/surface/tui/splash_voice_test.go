package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"
	"github.com/tristanbietsch/rex/internal/features/audio"
)

func TestSplashHandOffRequestsBootVoice(t *testing.T) {
	bootSequence = make([]stepFunc, 1)
	bootSequence[0] = func(_ Model) tea.Cmd { return nil }
	rec := &recordingAudio{}
	m := Model{Focus: FocusBoot, BootStart: time.Now(), Audio: rec}

	mi, _ := m.appendBootStep(bootStepMsg{Name: "x", Status: stepOK})
	m = mi.(Model)
	_, _ = m.Update(bootMinElapsedMsg{})

	require.Equal(t, []string{audio.EventBootOK, audio.EventStartup, audio.EventBootVoice}, rec.Events,
		"voice is requested right after the startup tone")
}
