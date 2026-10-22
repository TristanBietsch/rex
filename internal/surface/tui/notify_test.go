package tui

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/features/audio"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// notifyModel builds a board model holding one session "s1" in state st, a
// recording audio fake, and a controllable clock.
func notifyModel(t *testing.T, st protocol.State) (Model, *recordingAudio, *time.Time) {
	t.Helper()
	rec := &recordingAudio{}
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	m := Model{
		Focus:    FocusBoard,
		Sessions: []protocol.SessionSummary{{ID: "s1", Slug: "one", State: st}},
		Audio:    rec,
		Store:    settings.NewStore(),
	}
	m.Clock = func() time.Time { return now }
	return m, rec, &now
}

func stateUpdate(t *testing.T, id string, st protocol.State) protocol.Envelope {
	t.Helper()
	raw, err := json.Marshal(protocol.SessionUpdated{SessionID: id, Patch: map[string]any{"state": string(st)}})
	require.NoError(t, err)
	return protocol.Envelope{Type: protocol.EventSessionUpdated, Data: raw}
}

func TestStateChangeNotifyPerTransition(t *testing.T) {
	cases := []struct {
		name     string
		from, to protocol.State
		want     []string
	}{
		{"needs_input to working resumes", protocol.StateNeedsInput, protocol.StateWorking, []string{audio.EventResume}},
		{"needs_input to done plays done once", protocol.StateNeedsInput, protocol.StateDone, []string{audio.EventDone}},
		{"working to needs_input asks for attention", protocol.StateWorking, protocol.StateNeedsInput, []string{audio.EventAttention}},
		{"working to done plays done", protocol.StateWorking, protocol.StateDone, []string{audio.EventDone}},
		{"queued to needs_input is silent", protocol.StateQueued, protocol.StateNeedsInput, nil},
		{"queued to working is silent", protocol.StateQueued, protocol.StateWorking, nil},
		{"needs_input to crashed is silent", protocol.StateNeedsInput, protocol.StateCrashed, nil},
		{"done to done is silent", protocol.StateDone, protocol.StateDone, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, rec, _ := notifyModel(t, tc.from)
			m = m.applyEvent(stateUpdate(t, "s1", tc.to))
			require.Equal(t, tc.to, m.Sessions[0].State)
			require.Equal(t, tc.want, rec.Events)
		})
	}
}

func TestStateChangeNotifyDebounce(t *testing.T) {
	m, rec, now := notifyModel(t, protocol.StateNeedsInput)

	// Heuristic flicker: needs_input↔working inside the window → one sound.
	m = m.applyEvent(stateUpdate(t, "s1", protocol.StateWorking))
	*now = now.Add(300 * time.Millisecond)
	m = m.applyEvent(stateUpdate(t, "s1", protocol.StateNeedsInput))
	*now = now.Add(300 * time.Millisecond)
	m = m.applyEvent(stateUpdate(t, "s1", protocol.StateWorking))
	require.Equal(t, []string{audio.EventResume}, rec.Events)

	// Past the window (measured from the last played sound) → plays again.
	*now = now.Add(notifyDebounce)
	m = m.applyEvent(stateUpdate(t, "s1", protocol.StateNeedsInput))
	require.Equal(t, []string{audio.EventResume, audio.EventAttention}, rec.Events)

	// Done is never debounced: completion must always be heard.
	*now = now.Add(100 * time.Millisecond)
	_ = m.applyEvent(stateUpdate(t, "s1", protocol.StateDone))
	require.Equal(t, []string{audio.EventResume, audio.EventAttention, audio.EventDone}, rec.Events)
}

func TestStateChangeNotifyDebounceIsPerSession(t *testing.T) {
	m, rec, _ := notifyModel(t, protocol.StateNeedsInput)
	m.Sessions = append(m.Sessions, protocol.SessionSummary{ID: "s2", State: protocol.StateWorking})

	m = m.applyEvent(stateUpdate(t, "s1", protocol.StateWorking))
	_ = m.applyEvent(stateUpdate(t, "s2", protocol.StateNeedsInput))
	require.Equal(t, []string{audio.EventResume, audio.EventAttention}, rec.Events)
}

func TestStateChangeNotifySnapshotSilent(t *testing.T) {
	m, rec, _ := notifyModel(t, protocol.StateNeedsInput)

	// A snapshot carrying changed states (reconnect / boot) plays nothing.
	raw, err := json.Marshal(protocol.Snapshot{Sessions: []protocol.SessionSummary{
		{ID: "s1", State: protocol.StateWorking},
		{ID: "s9", State: protocol.StateNeedsInput},
	}})
	require.NoError(t, err)
	m = m.applyEvent(protocol.Envelope{Type: protocol.EventSnapshot, Data: raw})
	require.Empty(t, rec.Events)

	// An update for a session the TUI never saw has no known prior state.
	m = m.applyEvent(stateUpdate(t, "ghost", protocol.StateWorking))
	require.Empty(t, rec.Events)

	// Live transitions after the snapshot still notify.
	_ = m.applyEvent(stateUpdate(t, "s9", protocol.StateWorking))
	require.Equal(t, []string{audio.EventResume}, rec.Events)
}

func TestStateChangeNotifySettingOff(t *testing.T) {
	m, rec, _ := notifyModel(t, protocol.StateNeedsInput)
	require.NoError(t, m.Store.Set("notify_on_state_change", false))

	m = m.applyEvent(stateUpdate(t, "s1", protocol.StateWorking))
	m = m.applyEvent(stateUpdate(t, "s1", protocol.StateNeedsInput))
	require.Empty(t, rec.Events)

	// The existing done sound is governed by sound_enabled, not this toggle.
	_ = m.applyEvent(stateUpdate(t, "s1", protocol.StateDone))
	require.Equal(t, []string{audio.EventDone}, rec.Events)
}

func TestStateChangeNotifyNilStoreDefaultsOn(t *testing.T) {
	m, rec, _ := notifyModel(t, protocol.StateWorking)
	m.Store = nil
	_ = m.applyEvent(stateUpdate(t, "s1", protocol.StateNeedsInput))
	require.Equal(t, []string{audio.EventAttention}, rec.Events)
}

func TestStateChangeNotifyPrunesOnRemove(t *testing.T) {
	m, _, _ := notifyModel(t, protocol.StateNeedsInput)
	m = m.applyEvent(stateUpdate(t, "s1", protocol.StateWorking))
	require.Contains(t, m.lastNotify, "s1")

	raw, err := json.Marshal(protocol.SessionRemoved{SessionID: "s1"})
	require.NoError(t, err)
	m = m.applyEvent(protocol.Envelope{Type: protocol.EventSessionRemoved, Data: raw})
	require.NotContains(t, m.lastNotify, "s1")
}
