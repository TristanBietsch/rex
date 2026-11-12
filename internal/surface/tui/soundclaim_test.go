package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/features/audio"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// mutableAudio is a recording fake that can report itself inaudible (muted,
// volume 0, no device), like *audio.Player.Audible.
type mutableAudio struct {
	recordingAudio
	muted bool
}

func (a *mutableAudio) Play(e string) {
	if !a.muted {
		a.recordingAudio.Play(e)
	}
}
func (a *mutableAudio) Audible(string) bool { return !a.muted }

// claimBoard is one open board: its model, what it played, its clock.
type claimBoard struct {
	m   Model
	rec *mutableAudio
}

// newClaimBoard builds a board holding sessions, sharing the claim folder
// dir with every other board built on it.
func newClaimBoard(dir string, now *time.Time, sessions ...protocol.SessionSummary) *claimBoard {
	rec := &mutableAudio{}
	m := Model{
		Focus:       FocusBoard,
		Filter:      "all",
		Sessions:    append([]protocol.SessionSummary(nil), sessions...),
		Audio:       rec,
		Store:       settings.NewStore(),
		SoundClaims: audio.NewClaims(dir),
	}
	m.Clock = func() time.Time { return *now }
	return &claimBoard{m: m, rec: rec}
}

// claimEnv encodes an event the way the daemon sends it to every board.
func claimEnv(t *testing.T, typ string, data any) protocol.Envelope {
	t.Helper()
	raw, err := json.Marshal(data)
	require.NoError(t, err)
	return protocol.Envelope{Type: typ, Data: raw}
}

// transition is a daemon state patch: state plus the daemon's timestamp
// (state.Store.Transition always sends last_event_at).
func transition(t *testing.T, id string, st protocol.State, at time.Time) protocol.Envelope {
	t.Helper()
	return claimEnv(t, protocol.EventSessionUpdated, protocol.SessionUpdated{
		SessionID: id, Patch: map[string]any{"state": string(st), "last_event_at": at},
	})
}

// fanOut hands env to every board, in order, as the daemon's fan-out does.
func fanOut(boards []*claimBoard, env protocol.Envelope) {
	for _, b := range boards {
		b.m = b.m.applyEvent(env)
	}
}

func played(boards ...*claimBoard) []string {
	var out []string
	for _, b := range boards {
		out = append(out, b.rec.Events...)
	}
	return out
}

// The reported bug: with N boards open, each played every session sound, a
// few ms apart (a double or an echo). With a shared claim folder, each
// daemon event plays once in total, whichever board wins.
func TestSoundClaims_TwoBoardsOneSoundPerEvent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sound-claims")
	now := time.Date(2026, 11, 12, 15, 0, 0, 0, time.UTC)
	s1 := protocol.SessionSummary{ID: "s1", Slug: "one", State: protocol.StateWorking}
	a, b := newClaimBoard(dir, &now, s1), newClaimBoard(dir, &now, s1)
	boards := []*claimBoard{a, b}

	step := func(env protocol.Envelope) {
		fanOut(boards, env)
		now = now.Add(2 * time.Second) // past the per-session debounce
	}
	step(transition(t, "s1", protocol.StateNeedsInput, now))
	step(transition(t, "s1", protocol.StateWorking, now))
	step(transition(t, "s1", protocol.StateNeedsInput, now)) // the next turn chimes again
	step(transition(t, "s1", protocol.StateDone, now))
	step(claimEnv(t, protocol.EventSessionAdded, protocol.SessionSummary{ID: "s2", Slug: "two", State: protocol.StateWorking}))
	step(claimEnv(t, protocol.EventSessionRemoved, protocol.SessionRemoved{SessionID: "s2"}))

	require.Equal(t, []string{
		audio.EventAttention, audio.EventResume, audio.EventAttention,
		audio.EventDone, audio.EventCreate, audio.EventDelete,
	}, played(a, b), "one sound per event across both boards")
	require.Empty(t, b.rec.Events, "the board that gets the event second never plays it")
}

// Mark done (retire_at patch) and a new question (wait_reason patch, no
// timestamp) also play once across boards.
func TestSoundClaims_MarkAndQuestionOnce(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sound-claims")
	now := time.Date(2026, 11, 12, 15, 0, 0, 0, time.UTC)
	s1 := protocol.SessionSummary{ID: "s1", Slug: "one", State: protocol.StateNeedsInput, WaitReason: protocol.WaitReview}
	a, b := newClaimBoard(dir, &now, s1), newClaimBoard(dir, &now, s1)
	boards := []*claimBoard{b, a}

	fanOut(boards, claimEnv(t, protocol.EventSessionUpdated, protocol.SessionUpdated{
		SessionID: "s1", Patch: map[string]any{"wait_reason": protocol.WaitInput},
	}))
	now = now.Add(2 * time.Second)
	fanOut(boards, claimEnv(t, protocol.EventSessionUpdated, protocol.SessionUpdated{
		SessionID: "s1", Patch: map[string]any{"retire_at": now.Add(time.Hour), "retired_at": now},
	}))
	require.Equal(t, []string{audio.EventAttention, audio.EventDone}, played(a, b))
}

// The losing board still records the debounce: a question patch right after
// a READY that another board played stays silent everywhere, as it does with
// one board.
func TestSoundClaims_LoserKeepsDebounce(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sound-claims")
	now := time.Date(2026, 11, 12, 15, 0, 0, 0, time.UTC)
	s1 := protocol.SessionSummary{ID: "s1", Slug: "one", State: protocol.StateWorking}
	a, b := newClaimBoard(dir, &now, s1), newClaimBoard(dir, &now, s1)
	boards := []*claimBoard{a, b}
	fanOut(boards, transition(t, "s1", protocol.StateNeedsInput, now))
	now = now.Add(500 * time.Millisecond)
	fanOut(boards, claimEnv(t, protocol.EventSessionUpdated, protocol.SessionUpdated{
		SessionID: "s1", Patch: map[string]any{"wait_reason": protocol.WaitInput},
	}))
	require.Equal(t, []string{audio.EventAttention}, played(a, b))
}

// audio_dedupe off: every board plays, as before.
func TestSoundClaims_SettingOff(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sound-claims")
	now := time.Date(2026, 11, 12, 15, 0, 0, 0, time.UTC)
	s1 := protocol.SessionSummary{ID: "s1", Slug: "one", State: protocol.StateWorking}
	a, b := newClaimBoard(dir, &now, s1), newClaimBoard(dir, &now, s1)
	for _, bd := range []*claimBoard{a, b} {
		require.NoError(t, bd.m.Store.Set("audio_dedupe", false))
	}
	fanOut([]*claimBoard{a, b}, transition(t, "s1", protocol.StateNeedsInput, now))
	require.Equal(t, []string{audio.EventAttention}, a.rec.Events)
	require.Equal(t, []string{audio.EventAttention}, b.rec.Events)
}

// A workspace board and the system board: a session outside the workspace
// plays on the system board even when the workspace board gets the event
// first (a filtered board never claims a sound it does not play); a session
// inside it plays once; with only the workspace board open, its sessions
// still play.
func TestSoundClaims_WorkspaceAndSystemBoard(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sound-claims")
	now := time.Date(2026, 11, 12, 15, 0, 0, 0, time.UTC)
	inA := protocol.SessionSummary{ID: "1", Slug: "in-a", State: protocol.StateWorking, WorkspaceID: wsA}
	inB := protocol.SessionSummary{ID: "2", Slug: "in-b", State: protocol.StateWorking, WorkspaceID: wsB}
	ws := newClaimBoard(dir, &now, inA, inB)
	ws.m.WS = wsScopeA()
	ws.m.wsTagging = true
	require.NoError(t, ws.m.Store.Set("workspaces", true))
	sys := newClaimBoard(dir, &now, inA, inB)
	boards := []*claimBoard{ws, sys} // the workspace board hears first

	fanOut(boards, transition(t, "2", protocol.StateNeedsInput, now))
	require.Empty(t, ws.rec.Events)
	require.Equal(t, []string{audio.EventAttention}, sys.rec.Events, "outside the workspace: the system board plays")

	fanOut(boards, transition(t, "1", protocol.StateNeedsInput, now))
	require.Equal(t, []string{audio.EventAttention}, ws.rec.Events, "inside: the first board plays")
	require.Len(t, sys.rec.Events, 1, "and the system board does not repeat it")

	// Only the workspace board is open.
	now = now.Add(2 * time.Second)
	fanOut([]*claimBoard{ws}, transition(t, "1", protocol.StateWorking, now))
	require.Equal(t, []string{audio.EventAttention, audio.EventResume}, ws.rec.Events)
}

// A muted board (sound off, volume 0, no audio device) does not take part:
// it never wins a claim and silences a board that would have played.
func TestSoundClaims_MutedBoardDoesNotClaim(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sound-claims")
	now := time.Date(2026, 11, 12, 15, 0, 0, 0, time.UTC)
	s1 := protocol.SessionSummary{ID: "s1", Slug: "one", State: protocol.StateWorking}
	muted, loud := newClaimBoard(dir, &now, s1), newClaimBoard(dir, &now, s1)
	muted.rec.muted = true
	fanOut([]*claimBoard{muted, loud}, transition(t, "s1", protocol.StateNeedsInput, now))
	require.Equal(t, []string{audio.EventAttention}, loud.rec.Events)
}

// A board whose claim folder is unusable (here a symlink) plays every sound:
// dedupe falls back to the old behavior and nothing crashes.
func TestSoundClaims_HostileFolderFallsBackToPlaying(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	require.NoError(t, os.Mkdir(target, 0o700))
	dir := filepath.Join(root, "sound-claims")
	if err := os.Symlink(target, dir); err != nil {
		t.Skipf("symlink: %v", err)
	}
	now := time.Date(2026, 11, 12, 15, 0, 0, 0, time.UTC)
	s1 := protocol.SessionSummary{ID: "s1", Slug: "one", State: protocol.StateWorking}
	a, b := newClaimBoard(dir, &now, s1), newClaimBoard(dir, &now, s1)
	fanOut([]*claimBoard{a, b}, transition(t, "s1", protocol.StateNeedsInput, now))
	require.Equal(t, []string{audio.EventAttention}, a.rec.Events)
	require.Equal(t, []string{audio.EventAttention}, b.rec.Events)
}

// No claimer (no home directory, tests): every sound plays.
func TestSoundClaims_NilClaimerPlays(t *testing.T) {
	m, rec, _ := notifyModel(t, protocol.StateWorking)
	m = m.applyEvent(stateUpdate(t, "s1", protocol.StateNeedsInput))
	require.Equal(t, []string{audio.EventAttention}, rec.Events)
}

// The key is the same for one daemon event on every board and differs
// between two transitions.
func TestPatchSoundKey(t *testing.T) {
	at := time.Date(2026, 11, 12, 15, 0, 0, 123456789, time.UTC)
	decode := func(env protocol.Envelope) protocol.SessionUpdated {
		var upd protocol.SessionUpdated
		require.NoError(t, json.Unmarshal(env.Data, &upd))
		return upd
	}
	k1 := patchSoundKey(decode(transition(t, "s1", protocol.StateNeedsInput, at)))
	k2 := patchSoundKey(decode(transition(t, "s1", protocol.StateNeedsInput, at)))
	require.Equal(t, k1, k2)
	require.NotEqual(t, k1, patchSoundKey(decode(transition(t, "s1", protocol.StateNeedsInput, at.Add(time.Nanosecond)))))
	require.NotEqual(t, k1, patchSoundKey(decode(transition(t, "s2", protocol.StateNeedsInput, at))))
}

// In-process doubles: one board that gets the same event twice (a replay on
// reconnect, a snapshot racing its add) plays it once.
func TestSoundClaims_ReplayOnOneBoardPlaysOnce(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sound-claims")
	now := time.Date(2026, 11, 12, 15, 0, 0, 0, time.UTC)
	s1 := protocol.SessionSummary{ID: "s1", Slug: "one", State: protocol.StateWorking}
	a := newClaimBoard(dir, &now, s1)
	boards := []*claimBoard{a}
	for _, env := range []protocol.Envelope{
		transition(t, "s1", protocol.StateNeedsInput, now),
		claimEnv(t, protocol.EventSessionAdded, protocol.SessionSummary{ID: "s2", Slug: "two", State: protocol.StateWorking}),
		claimEnv(t, protocol.EventSnapshot, protocol.Snapshot{Sessions: []protocol.SessionSummary{
			{ID: "s1", Slug: "one", State: protocol.StateNeedsInput},
			{ID: "s2", Slug: "two", State: protocol.StateWorking},
		}}),
	} {
		fanOut(boards, env)
		now = now.Add(2 * time.Second)
		fanOut(boards, env) // replayed
	}
	require.Equal(t, []string{audio.EventAttention, audio.EventCreate}, a.rec.Events)
}
