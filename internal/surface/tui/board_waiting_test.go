package tui

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

var waitNow = time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)

// waitingModel is a board at waitNow with the given sessions.
func waitingModel(sessions ...protocol.SessionSummary) Model {
	m := boardModel(sessions...)
	m.Clock = func() time.Time { return waitNow }
	return m
}

func waitingOff(m Model) Model {
	st := settings.NewStore()
	if err := st.Set("waiting_section", false); err != nil {
		panic(err)
	}
	m.Store = st
	return m
}

// waiter is a working session that has waited on kind for age.
func waiter(id, kind string, age time.Duration) protocol.SessionSummary {
	return protocol.SessionSummary{ID: id, Slug: id, ToolID: "claude", State: protocol.StateWorking,
		WaitingOn: kind, WaitingSince: waitNow.Add(-age)}
}

func groupIndexOf(t *testing.T, m Model, id string) int {
	t.Helper()
	got := -1
	for gi, rows := range partitionSessions(m.Sessions, rowFilterOf(m)) {
		for _, s := range rows {
			if s.ID == id {
				require.Equal(t, -1, got, "%s is in more than one group", id)
				got = gi
			}
		}
	}
	require.GreaterOrEqual(t, got, 0, "%s is in no group", id)
	return got
}

// Every state x wait kind x age x marked-done lands in exactly one group,
// the threshold decides Waiting, and needs_input is never in Waiting.
func TestGrouping_Table(t *testing.T) {
	states := []protocol.State{protocol.StateQueued, protocol.StateWorking, protocol.StateNeedsInput,
		protocol.StateDone, protocol.StateFailed, protocol.StateCrashed}
	kinds := []string{"", protocol.WaitShell, protocol.WaitSubagent, protocol.WaitWeb, "teleport"}
	ages := []time.Duration{0, protocol.WaitingAfter - time.Millisecond, protocol.WaitingAfter, time.Hour}
	for _, flagOn := range []bool{true, false} {
		for _, st := range states {
			for _, kind := range kinds {
				for _, age := range ages {
					for _, marked := range []bool{false, true} {
						name := fmt.Sprintf("flag=%v/%s/%q/%s/marked=%v", flagOn, st, kind, age, marked)
						s := protocol.SessionSummary{ID: "s", State: st, WaitingOn: kind, WaitingSince: waitNow.Add(-age)}
						if marked {
							s.RetireAt = waitNow.Add(time.Hour)
						}
						m := waitingModel(s)
						if !flagOn {
							m = waitingOff(m)
						}
						want := groupWorking
						switch {
						case marked || st == protocol.StateDone || st == protocol.StateFailed || st == protocol.StateCrashed:
							want = groupDone
						case st == protocol.StateNeedsInput:
							want = groupNeeds
						case flagOn && st == protocol.StateWorking && protocol.ValidWaitKind(kind) && age >= protocol.WaitingAfter:
							want = groupWaiting
						}
						require.Equal(t, want, groupIndexOf(t, m, "s"), name)
					}
				}
			}
		}
	}
}

func TestGrouping_ZeroWaitingSinceStaysWorking(t *testing.T) {
	m := waitingModel(protocol.SessionSummary{ID: "s", State: protocol.StateWorking, WaitingOn: protocol.WaitShell})
	require.Equal(t, groupWorking, groupIndexOf(t, m, "s"))
}

// A daemon that predates the fields sends none: everything working stays in
// Working.
func TestGrouping_OldClientViewStaysInWorking(t *testing.T) {
	var s protocol.SessionSummary
	require.NoError(t, json.Unmarshal([]byte(`{"id":"s","slug":"s","tool_id":"claude","state":"working","started_at":"2026-01-01T00:00:00Z","last_event_at":"2026-01-01T00:00:00Z"}`), &s))
	m := waitingModel(s)
	require.Equal(t, groupWorking, groupIndexOf(t, m, "s"))
	require.NotContains(t, ansi.Strip(renderHeader(m, 160)), "waiting")
}

// The board reads the clock: the same session moves into Waiting by itself
// when the wait reaches the threshold.
func TestGrouping_MovesAtThresholdByClock(t *testing.T) {
	m := waitingModel(waiter("s", protocol.WaitShell, 0))
	require.Equal(t, groupWorking, groupIndexOf(t, m, "s"))
	m.Clock = func() time.Time { return waitNow.Add(protocol.WaitingAfter - time.Millisecond) }
	require.Equal(t, groupWorking, groupIndexOf(t, m, "s"))
	m.Clock = func() time.Time { return waitNow.Add(protocol.WaitingAfter) }
	require.Equal(t, groupWaiting, groupIndexOf(t, m, "s"))
}

func waitingBoard() Model {
	return waitingModel(
		protocol.SessionSummary{ID: "n1", Slug: "n1", State: protocol.StateNeedsInput},
		waiter("w1", protocol.WaitShell, time.Minute),
		waiter("w2", protocol.WaitWeb, time.Hour),
		protocol.SessionSummary{ID: "r1", Slug: "r1", State: protocol.StateWorking},
		waiter("q1", protocol.WaitShell, time.Second), // not yet
		protocol.SessionSummary{ID: "d1", Slug: "d1", State: protocol.StateDone},
	)
}

func TestBoard_WaitingSectionBelowNeedsYou(t *testing.T) {
	m := waitingBoard()
	require.Equal(t, []string{"n1", "w1", "w2", "r1", "q1", "d1"}, sessionIDs(orderedSessions(m)),
		"screen order: needs, waiting, working, done")
	text := ansi.Strip(renderBoard(m, 120, 0))
	require.Regexp(t, `(?s)NEEDS YOU\s+1.*WAITING\s+2.*WORKING\s+2.*DONE\s+1`, text)
}

func TestBoard_EmptyWaitingSectionShowsPlaceholder(t *testing.T) {
	m := waitingModel(protocol.SessionSummary{ID: "r1", Slug: "r1", State: protocol.StateWorking})
	text := ansi.Strip(renderBoard(m, 120, 0))
	require.Contains(t, text, "WAITING")
	require.Contains(t, text, "nothing waiting on a tool")
}

func TestHeader_Counts(t *testing.T) {
	m := waitingBoard()
	require.Contains(t, ansi.Strip(renderHeader(m, 200)), "1 need you · 2 waiting · 2 working · 1 done")

	// Left out at 0, like an empty alert.
	none := waitingModel(protocol.SessionSummary{ID: "r1", State: protocol.StateWorking})
	require.Contains(t, ansi.Strip(renderHeader(none, 200)), "0 need you · 1 working · 0 done")
	require.NotContains(t, ansi.Strip(renderHeader(none, 200)), "waiting")

	for style, want := range map[string]string{
		"glyphs":  "◆1  ◷2  ⟳2  ●1",
		"numbers": "1 · 4 · 1",
	} {
		st := settings.NewStore()
		require.NoError(t, st.Set("header_style", style))
		sm := waitingBoard()
		sm.Store = st
		require.Contains(t, ansi.Strip(renderHeader(sm, 200)), want, style)
	}
	st := settings.NewStore()
	require.NoError(t, st.Set("header_style", "glyphs"))
	gm := none
	gm.Store = st
	require.Contains(t, ansi.Strip(renderHeader(gm, 200)), "◆0  ⟳1  ●0")
}

func TestSaverCounts_IncludeWaiting(t *testing.T) {
	m := waitingBoard()
	require.Equal(t, "1 need you · 2 waiting · 2 working · 1 done", ansi.Strip(saverCounts(m)))
	none := waitingModel(protocol.SessionSummary{ID: "r1", State: protocol.StateWorking})
	require.Equal(t, "0 need you · 1 working · 0 done", ansi.Strip(saverCounts(none)))
}

func TestWaitingNeverInAlert(t *testing.T) {
	require.False(t, boardGroups[groupWaiting].Alert)
}

func TestNumberKeysFollowScreenOrder(t *testing.T) {
	m := waitingBoard()
	for _, c := range []struct{ key, want string }{
		{"1", "n1"}, {"2", "w1"}, {"3", "r1"}, {"4", "d1"},
	} {
		got, _ := updateKey(m, key(c.key))
		require.Equal(t, c.want, got.SelectedID, c.key)
	}
	got, _ := updateKey(m, key("5"))
	require.Equal(t, m.SelectedID, got.SelectedID, "5 is not a section")
}

func TestFlagOff_WaitingSessionsStayInWorking(t *testing.T) {
	m := waitingOff(waitingBoard())
	require.Equal(t, groupWorking, groupIndexOf(t, m, "w1"))
	require.Equal(t, groupWorking, groupIndexOf(t, m, "w2"))
	require.Equal(t, []string{"n1", "w1", "w2", "r1", "q1", "d1"}, sessionIDs(orderedSessions(m)))

	text := ansi.Strip(renderBoard(m, 120, 0))
	require.NotContains(t, text, "WAITING")
	require.NotContains(t, text, "nothing waiting on a tool")
	require.Regexp(t, `(?s)NEEDS YOU\s+1.*WORKING\s+4.*DONE\s+1`, text)

	header := ansi.Strip(renderHeader(m, 200))
	require.Contains(t, header, "1 need you · 4 working · 1 done")
	require.NotContains(t, header, "waiting")
	require.Equal(t, "1 need you · 4 working · 1 done", ansi.Strip(saverCounts(m)))
}

// With the feature off the keys revert: 1 needs, 2 working, 3 done; 4 does
// nothing.
func TestFlagOff_KeysRevert(t *testing.T) {
	m := waitingOff(waitingBoard())
	for _, c := range []struct{ key, want string }{
		{"1", "n1"}, {"2", "w1"}, {"3", "d1"},
	} {
		got, _ := updateKey(m, key(c.key))
		require.Equal(t, c.want, got.SelectedID, c.key)
	}
	got, _ := updateKey(m, key("4"))
	require.Equal(t, m.SelectedID, got.SelectedID, "4 does nothing")

	_, err := resolveSection(m, "waiting")
	require.Error(t, err)
	gi, err := resolveSection(m, "2")
	require.NoError(t, err)
	require.Equal(t, groupWorking, gi)
	gi, err = resolveSection(m, "w")
	require.NoError(t, err, "w is unique again")
	require.Equal(t, groupWorking, gi)

	m = press(m, "z", "3")
	require.True(t, m.Hidden["done"], "z3 is Done")
	m = press(waitingOff(waitingBoard()), "z", "4")
	require.Equal(t, "no section 4: use z1-z3", m.Notice)
}

// A hide list that names every section the board has is ignored, so a
// stale list never blanks the board when Waiting is switched off.
func TestFlagOff_StaleHideListNeverBlanksBoard(t *testing.T) {
	m := waitingOff(waitingBoard())
	m.Hidden = map[string]bool{"needs": true, "working": true, "done": true}
	require.Equal(t, 6, len(orderedSessions(m)))
	m = waitingBoard()
	m.Hidden = map[string]bool{"needs": true, "working": true, "done": true}
	require.Equal(t, []string{"w1", "w2"}, sessionIDs(orderedSessions(m)), "Waiting is still shown with the feature on")
}

func TestWaitingSectionHideable(t *testing.T) {
	m := setSectionHidden(waitingBoard(), "waiting", true)
	require.Equal(t, []string{"n1", "r1", "q1", "d1"}, sessionIDs(orderedSessions(m)))
	m = press(waitingBoard(), "z", "2")
	require.True(t, m.Hidden["waiting"])
	m, _ = updateKey(m, key("2"))
	require.Equal(t, "Waiting is hidden — z2 to show", m.Notice)
	m, _ = executeCommand(waitingBoard(), "filter hide waiting")
	require.True(t, m.Hidden["waiting"])
}

func TestFilterByGroupTotalUsesGrouping(t *testing.T) {
	m := waitingBoard()
	m.Search = "w1"
	m.Store = settings.NewStore()
	title := ansi.Strip(groupTitleText(m, groupWaiting))
	require.Contains(t, title, "WAITING  1/2")
}
