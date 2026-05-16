package tui

import (
	"errors"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"
	"github.com/tristanbietsch/rex/internal/audio"
)

func TestSplashChimePerStatus(t *testing.T) {
	bootSequence = make([]stepFunc, 4)
	for i := range bootSequence {
		bootSequence[i] = func(_ Model) tea.Cmd { return nil }
	}

	rec := &recordingAudio{}
	m := Model{Focus: FocusBoot, BootStart: time.Now(), Audio: rec}

	mi, _ := m.appendBootStep(bootStepMsg{Name: "a", Status: stepOK})
	m = mi.(Model)
	mi, _ = m.appendBootStep(bootStepMsg{Name: "b", Status: stepWarn})
	m = mi.(Model)
	mi, _ = m.appendBootStep(bootStepMsg{Name: "c", Status: stepSkip})
	m = mi.(Model)
	mi, _ = m.appendBootStep(bootStepMsg{Name: "d", Status: stepFail, Err: errors.New("x")})
	m = mi.(Model)

	require.Equal(t, []string{
		audio.EventBootOK,
		audio.EventBootWarn,
		audio.EventBootFail,
	}, rec.Events, "OK + WARN + FAIL chime; SKIP is silent")
}

func TestSplashHandOffPlaysStartup(t *testing.T) {
	bootSequence = make([]stepFunc, 1)
	bootSequence[0] = func(_ Model) tea.Cmd { return nil }
	rec := &recordingAudio{}
	m := Model{Focus: FocusBoot, BootStart: time.Now(), Audio: rec}

	mi, _ := m.appendBootStep(bootStepMsg{Name: "x", Status: stepOK})
	m = mi.(Model)
	mi, _ = m.update(bootMinElapsedMsg{})
	m = mi.(Model)

	require.Equal(t, FocusBoard, m.Focus)
	require.Contains(t, rec.Events, audio.EventStartup)
}
