package tui

import (
	"errors"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"
)

// recordingAudio is a recording audioPlayer fake.
type recordingAudio struct{ Events []string }

func (r *recordingAudio) Play(e string)        { r.Events = append(r.Events, e) }
func (r *recordingAudio) SetVolume(_ float64)  {}
func (r *recordingAudio) SetEnabled(_ bool)    {}
func (r *recordingAudio) SetSoundset(_ string) {}

func newBootModelWithSeq(steps int) Model {
	bootSequence = make([]stepFunc, steps)
	for i := range bootSequence {
		bootSequence[i] = func(_ Model) tea.Cmd { return nil }
	}
	return Model{
		Focus:     FocusBoot,
		BootStart: time.Now(),
		Audio:     &recordingAudio{},
	}
}

func TestBootPipelineAdvances(t *testing.T) {
	m := newBootModelWithSeq(3)
	mi, _ := m.appendBootStep(bootStepMsg{Name: "a", Status: stepOK})
	m = mi.(Model)
	require.Equal(t, 1, m.BootStep)
	require.False(t, m.BootDone)

	mi, _ = m.appendBootStep(bootStepMsg{Name: "b", Status: stepOK})
	m = mi.(Model)
	require.Equal(t, 2, m.BootStep)
	require.False(t, m.BootDone)

	mi, _ = m.appendBootStep(bootStepMsg{Name: "c", Status: stepOK})
	m = mi.(Model)
	require.Equal(t, 3, m.BootStep)
	require.True(t, m.BootDone)
}

func TestBootHandOffWaitsForMin(t *testing.T) {
	m := newBootModelWithSeq(1)
	mi, _ := m.appendBootStep(bootStepMsg{Name: "x", Status: stepOK})
	m = mi.(Model)
	require.True(t, m.BootDone)
	require.False(t, m.BootMinDone)
	require.Equal(t, FocusBoot, m.Focus, "no handoff before min")

	mi2, cmd := m.update(bootMinElapsedMsg{})
	m = mi2.(Model)
	require.True(t, m.BootMinDone)
	require.Equal(t, FocusBoard, m.Focus)
	require.NotNil(t, cmd, "handoff batch should be returned")
}

func TestBootHandOffWaitsForLastStep(t *testing.T) {
	m := newBootModelWithSeq(2)
	mi, _ := m.update(bootMinElapsedMsg{})
	m = mi.(Model)
	require.True(t, m.BootMinDone)
	require.Equal(t, FocusBoot, m.Focus, "no handoff while pipeline running")

	mi, _ = m.appendBootStep(bootStepMsg{Name: "a", Status: stepOK})
	m = mi.(Model)
	require.Equal(t, FocusBoot, m.Focus)

	mi, _ = m.appendBootStep(bootStepMsg{Name: "b", Status: stepOK})
	m = mi.(Model)
	require.Equal(t, FocusBoard, m.Focus, "handoff fires on last step")
}

func TestBootFailureBlocksHandOff(t *testing.T) {
	m := newBootModelWithSeq(3)
	failErr := errors.New("daemon not found")
	mi, _ := m.appendBootStep(bootStepMsg{Name: "daemon", Status: stepFail, Err: failErr})
	m = mi.(Model)
	require.True(t, m.BootFailed)
	require.Equal(t, failErr, m.BootError)

	mi2, _ := m.update(bootMinElapsedMsg{})
	m = mi2.(Model)
	require.True(t, m.BootMinDone)
	require.Equal(t, FocusBoot, m.Focus, "still on splash after fail+min")
}

func TestBootHandOffClearsLog(t *testing.T) {
	m := newBootModelWithSeq(1)
	mi, _ := m.appendBootStep(bootStepMsg{Name: "only", Status: stepOK})
	m = mi.(Model)
	mi, _ = m.update(bootMinElapsedMsg{})
	m = mi.(Model)
	require.Equal(t, FocusBoard, m.Focus)
	require.Nil(t, m.BootLog)
}
