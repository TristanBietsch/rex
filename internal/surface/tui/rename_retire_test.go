package tui

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

type fakeNamer struct {
	calls []string
}

func (f *fakeNamer) SetSessionName(id, name, _ string) error {
	f.calls = append(f.calls, id+"="+name)
	return nil
}

type fakeRetirer struct {
	retired   []string
	unretired []string
	ttl       time.Duration
}

func (f *fakeRetirer) Retire(id string, ttl time.Duration, _ string) error {
	f.retired = append(f.retired, id)
	f.ttl = ttl
	return nil
}

func (f *fakeRetirer) Unretire(id, _ string) error {
	f.unretired = append(f.unretired, id)
	return nil
}

func boardModel(sessions ...protocol.SessionSummary) Model {
	m := Model{Focus: FocusBoard, Filter: "all", Width: 160, Height: 40, Sessions: sessions}
	if len(sessions) > 0 {
		m.SelectedID = sessions[0].ID
	}
	return m
}

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	case "ctrl+u":
		return tea.KeyMsg{Type: tea.KeyCtrlU}
	case "ctrl+w":
		return tea.KeyMsg{Type: tea.KeyCtrlW}
	case " ":
		return tea.KeyMsg{Type: tea.KeySpace}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func typeText(m Model, s string) Model {
	for _, r := range s {
		m, _ = updateKey(m, key(string(r)))
	}
	return m
}

func runCmd(cmd tea.Cmd) {
	if cmd != nil {
		cmd()
	}
}

func TestRename_OpenEditSubmit(t *testing.T) {
	fn := &fakeNamer{}
	m := boardModel(protocol.SessionSummary{ID: "a", Slug: "cc.opus.x", State: protocol.StateNeedsInput, Name: "old"})
	m.namer = fn

	m, _ = updateKey(m, key("r"))
	require.Equal(t, FocusRename, m.Focus)
	require.Equal(t, "old", m.Rename.Draft, "draft pre-filled with current name")

	m, _ = updateKey(m, key("ctrl+u"))
	m = typeText(m, "auth  fix")
	m, _ = updateKey(m, key("backspace"))
	m, cmd := updateKey(m, key("enter"))
	runCmd(cmd)
	require.Equal(t, FocusBoard, m.Focus)
	require.Equal(t, []string{"a=auth fi"}, fn.calls, "name normalized (whitespace collapsed)")
}

func TestRename_EscAndUnchangedSendNothing(t *testing.T) {
	fn := &fakeNamer{}
	m := boardModel(protocol.SessionSummary{ID: "a", Slug: "s", State: protocol.StateDone, Name: "keep"})
	m.namer = fn

	m, _ = updateKey(m, key("r"))
	m = typeText(m, "zzz")
	m, _ = updateKey(m, key("esc"))
	require.Equal(t, FocusBoard, m.Focus)
	require.Nil(t, m.Rename)

	m, _ = updateKey(m, key("r"))
	m, cmd := updateKey(m, key("enter"))
	runCmd(cmd)
	require.Empty(t, fn.calls)
}

func TestRename_EmptyClearsAndCap(t *testing.T) {
	fn := &fakeNamer{}
	m := boardModel(protocol.SessionSummary{ID: "a", Slug: "s", State: protocol.StateDone, Name: "x"})
	m.namer = fn
	m, _ = updateKey(m, key("r"))
	m = typeText(m, strings.Repeat("y", protocol.MaxSessionNameRunes+10))
	require.Equal(t, protocol.MaxSessionNameRunes, len([]rune(m.Rename.Draft)), "input capped at the limit")

	m, _ = updateKey(m, key("ctrl+u"))
	_, cmd := updateKey(m, key("enter"))
	runCmd(cmd)
	require.Equal(t, []string{"a="}, fn.calls)
}

func TestRename_NoSelectionNoop(t *testing.T) {
	m := boardModel()
	m, _ = updateKey(m, key("r"))
	require.Equal(t, FocusBoard, m.Focus)
}

func TestRename_PromptNeverWraps(t *testing.T) {
	m := boardModel(protocol.SessionSummary{ID: "a", Slug: "slug", State: protocol.StateDone})
	m, _ = updateKey(m, key("r"))
	m = typeText(m, strings.Repeat("界", 60))
	for _, w := range []int{60, 200} {
		line := renderRenamePrompt(m, w)
		require.NotContains(t, line, "\n")
		require.LessOrEqual(t, ansi.StringWidth(line), w)
	}
}

func TestRetireToggle_MarkAndUndo(t *testing.T) {
	fr := &fakeRetirer{}
	store := settings.NewStore()
	require.NoError(t, store.Set("retire_after_hours", 2))
	m := boardModel(protocol.SessionSummary{ID: "a", Slug: "s", State: protocol.StateNeedsInput})
	m.retirer = fr
	m.Store = store

	m, cmd := updateKey(m, key("c"))
	runCmd(cmd)
	require.Equal(t, []string{"a"}, fr.retired)
	require.Equal(t, 2*time.Hour, fr.ttl)
	require.Contains(t, m.Notice, "clears in 2h")

	m.Sessions[0].RetireAt = time.Now().Add(time.Hour)
	_, cmd = updateKey(m, key("c"))
	runCmd(cmd)
	require.Equal(t, []string{"a"}, fr.unretired)
}

func TestApplyPatch_TimesSetAndClear(t *testing.T) {
	s := protocol.SessionSummary{}
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	raw, _ := json.Marshal(map[string]any{"retire_at": at, "retired_at": at, "working_since": at, "name": "n"})
	var patch map[string]any
	require.NoError(t, json.Unmarshal(raw, &patch))
	applyPatch(&s, patch)
	require.True(t, at.Equal(s.RetireAt))
	require.True(t, at.Equal(s.RetiredAt))
	require.True(t, at.Equal(s.WorkingSince))
	require.Equal(t, "n", s.Name)

	applyPatch(&s, map[string]any{"retire_at": nil, "retired_at": nil, "working_since": nil, "name": ""})
	require.True(t, s.RetireAt.IsZero())
	require.True(t, s.RetiredAt.IsZero())
	require.True(t, s.WorkingSince.IsZero())
	require.Equal(t, "", s.Name)

	applyPatch(&s, map[string]any{"tokens": 3.0})
	require.True(t, s.RetireAt.IsZero(), "absent key leaves field alone")
}

func TestRetireLabel(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	mk := func(d time.Duration, st protocol.State) protocol.SessionSummary {
		return protocol.SessionSummary{State: st, RetireAt: now.Add(d)}
	}
	require.Equal(t, "", retireLabel(now, protocol.SessionSummary{}))
	require.Equal(t, "marked done · clears in 6d", retireLabel(now, mk(6*24*time.Hour+time.Hour, protocol.StateDone)))
	require.Equal(t, "marked done · clears in 30h", retireLabel(now, mk(30*time.Hour, protocol.StateDone)))
	require.Equal(t, "marked done · clears in 4m", retireLabel(now, mk(4*time.Minute+5*time.Second, protocol.StateDone)))
	require.Equal(t, "marked done · clearing…", retireLabel(now, mk(-time.Second, protocol.StateDone)))
	require.Equal(t, "marked done · clears when idle", retireLabel(now, mk(-time.Second, protocol.StateWorking)))
}

func TestDoneBadgeText(t *testing.T) {
	now := time.Date(2026, 10, 22, 18, 0, 0, 0, time.UTC)
	mk := func(at time.Time) protocol.SessionSummary {
		return protocol.SessionSummary{RetireAt: now.Add(time.Hour), RetiredAt: at}
	}
	require.Equal(t, "done 14:32", doneBadgeText(now, mk(time.Date(2026, 10, 22, 14, 32, 0, 0, time.UTC))))
	require.Equal(t, "done Oct 21", doneBadgeText(now, mk(time.Date(2026, 10, 21, 23, 59, 0, 0, time.UTC))))
	require.Equal(t, "done Oct 22", doneBadgeText(now, mk(time.Date(2025, 10, 22, 9, 0, 0, 0, time.UTC))), "other year is not today")
	require.Equal(t, "done", doneBadgeText(now, protocol.SessionSummary{RetireAt: now.Add(time.Hour)}), "legacy mark has no time")
	require.Equal(t, "", doneBadge(now, protocol.SessionSummary{}))
}

func TestMarkedDone_MovesToCompleted(t *testing.T) {
	now := time.Now()
	m := boardModel(
		protocol.SessionSummary{ID: "n", Slug: "n", State: protocol.StateNeedsInput},
		protocol.SessionSummary{ID: "w", Slug: "w", State: protocol.StateWorking, RetireAt: now.Add(time.Hour), RetiredAt: now},
		protocol.SessionSummary{ID: "d", Slug: "d", State: protocol.StateDone},
	)
	parts := partitionSessions(m.Sessions, "all")
	require.Len(t, parts[0], 1)
	require.Len(t, parts[1], 0, "marked working session leaves Working")
	require.Len(t, parts[2], 2)
	var ids []string
	for _, s := range orderedSessions(m) {
		ids = append(ids, s.ID)
	}
	require.Equal(t, []string{"n", "w", "d"}, ids)
	m.Sessions[1].RetireAt = time.Time{}
	require.Len(t, partitionSessions(m.Sessions, "all")[1], 1, "unmarking returns it to Working")
}

func TestLongRunTiers(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	w := func(d time.Duration) protocol.SessionSummary {
		return protocol.SessionSummary{State: protocol.StateWorking, WorkingSince: now.Add(-d)}
	}
	require.Equal(t, -1, longRunTier(now, w(5*time.Minute)))
	require.Equal(t, 3, longRunTier(now, w(12*time.Minute)))
	require.Equal(t, 2, longRunTier(now, w(45*time.Minute)))
	require.Equal(t, 1, longRunTier(now, w(3*time.Hour)))
	require.Equal(t, 0, longRunTier(now, w(7*time.Hour)))
	idle := w(7 * time.Hour)
	idle.State = protocol.StateNeedsInput
	require.Equal(t, -1, longRunTier(now, idle), "only working sessions get the badge")
	require.Contains(t, longRunBadge(now, w(45*time.Minute)), "▴45m")
}

func TestBoardRow_NameRetireAndBadgeFitWidth(t *testing.T) {
	now := time.Now()
	m := boardModel(
		protocol.SessionSummary{ID: "a", ShortID: "aaaa", Slug: "cc.opus.slug", Name: "日本語の長いタスク名前です", ModelID: "opus",
			State: protocol.StateWorking, WorkingSince: now.Add(-3 * time.Hour), RetireAt: now.Add(-time.Minute), LastEventAt: now},
		protocol.SessionSummary{ID: "b", ShortID: "bbbb", Slug: "plain", ModelID: "opus", State: protocol.StateDone, LastEventAt: now},
	)
	m.Clock = func() time.Time { return now }
	for _, w := range []int{80, 160} {
		cols := boardLayout(m, w)
		for _, s := range m.Sessions {
			row := renderRow(m, s, cols)
			require.NotContains(t, row, "\n")
			require.Equal(t, w, ansi.StringWidth(row), "width %d", w)
		}
	}
	cols := boardLayout(m, 160)
	row := ansi.Strip(renderRow(m, m.Sessions[0], cols))
	require.Contains(t, row, "日本語")
	require.Contains(t, row, "done", "legacy mark without time shows bare badge")
	require.NotContains(t, row, "clears")
	require.Contains(t, row, "▴3h")
	require.Contains(t, ansi.Strip(renderRow(m, m.Sessions[1], cols)), "plain")
}

func TestSessionRemoved_EndsAttachAndReselects(t *testing.T) {
	m := boardModel(
		protocol.SessionSummary{ID: "a", Slug: "a", State: protocol.StateDone},
		protocol.SessionSummary{ID: "b", Slug: "b", State: protocol.StateDone},
	)
	m.Attach = &AttachState{SessionID: "a"}
	raw, _ := json.Marshal(protocol.SessionRemoved{SessionID: "a", Reason: protocol.RemovedRetired})
	m = m.applyEvent(protocol.Envelope{Type: protocol.EventSessionRemoved, Data: raw})
	require.True(t, m.Attach.Ended)
	require.Contains(t, m.Attach.EndedMsg, "marked done")
	require.Equal(t, "b", m.SelectedID)
	require.Contains(t, m.Notice, "cleared a")
}

func TestFriendlyDaemonError_OldDaemon(t *testing.T) {
	got := friendlyDaemonError(protocol.ErrorEvent{Code: protocol.ErrCodeBadIntent, Message: "intent not implemented"})
	require.Contains(t, got, "rex daemon restart")
}

func TestSpinner_RearmsWhenSessionStartsWorking(t *testing.T) {
	m := boardModel(protocol.SessionSummary{ID: "a", Slug: "a", State: protocol.StateNeedsInput})
	// Chain stops when nothing is working.
	next, cmd := m.Update(SpinnerTickMsg{Gen: 0})
	m = next.(Model)
	require.Nil(t, cmd)
	require.False(t, m.spinnerRunning)

	// Session flips to working → spinner re-armed with a fresh chain.
	m.Sessions[0].State = protocol.StateWorking
	m, cmd = armSpinner(m)
	require.NotNil(t, cmd)
	require.True(t, m.spinnerRunning)
	gen := m.spinnerGen

	// Stale chain ticks are dropped; current chain advances and re-arms.
	tick := m.SpinnerTick
	next, cmd = m.Update(SpinnerTickMsg{Gen: gen - 1})
	m = next.(Model)
	require.Nil(t, cmd)
	require.Equal(t, tick, m.SpinnerTick)
	next, cmd = m.Update(SpinnerTickMsg{Gen: gen})
	m = next.(Model)
	require.NotNil(t, cmd)
	require.Equal(t, tick+1, m.SpinnerTick)

	// Arming again while running is a no-op (no parallel chains).
	_, cmd = armSpinner(m)
	require.Nil(t, cmd)
}

func TestCommand_NameDoneComplete(t *testing.T) {
	fn := &fakeNamer{}
	fr := &fakeRetirer{}
	m := boardModel(protocol.SessionSummary{ID: "a", ShortID: "aa", Slug: "s", State: protocol.StateDone})
	m.namer, m.retirer = fn, fr

	_, cmd := executeCommand(m, "name  my  task ")
	runCmd(cmd)
	require.Equal(t, []string{"a=my  task"}, fn.calls, "daemon normalizes; TUI sends trimmed text")

	_, cmd = executeCommand(m, "done aa")
	runCmd(cmd)
	require.Equal(t, []string{"a"}, fr.retired)

	m2, _ := executeCommand(m, "done nope")
	require.Contains(t, m2.Err, "no match")

	m3, _ := executeCommand(m, "name")
	require.Equal(t, FocusRename, m3.Focus)
}

func TestApplyPatch_LastEventAt(t *testing.T) {
	s := protocol.SessionSummary{}
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	raw, _ := json.Marshal(map[string]any{"last_event_at": at})
	var patch map[string]any
	require.NoError(t, json.Unmarshal(raw, &patch))
	applyPatch(&s, patch)
	require.True(t, at.Equal(s.LastEventAt), "board time-ago must follow live activity")
}

func TestHeader_NeverWrapsAtNarrowWidths(t *testing.T) {
	now := time.Now()
	m := boardModel(
		protocol.SessionSummary{ID: "a", Slug: "a", State: protocol.StateDone, RetireAt: now.Add(time.Hour)},
		protocol.SessionSummary{ID: "b", Slug: "b", State: protocol.StateFailed},
	)
	m.BackendUnavailable = true
	for _, w := range []int{40, 60, 80} {
		out := renderHeader(m, w)
		lines := strings.Split(out, "\n")
		require.Len(t, lines, headerHeight(m), "width %d", w)
		for _, l := range lines {
			require.LessOrEqual(t, ansi.StringWidth(l), w)
		}
	}
}
