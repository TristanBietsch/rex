package tui

import (
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// ucModel has a marked live row (a), an unmarked live row (b), an exited
// row (c) and a marked, exited row (d). Selection starts on a.
func ucModel() (Model, *fakeRetirer) {
	fr := &fakeRetirer{}
	marked := time.Now().Add(time.Hour)
	m := boardModel(
		protocol.SessionSummary{ID: "a", Slug: "alpha", State: protocol.StateWorking, RetireAt: marked},
		protocol.SessionSummary{ID: "b", Slug: "beta", State: protocol.StateNeedsInput},
		protocol.SessionSummary{ID: "c", Slug: "gamma", State: protocol.StateDone},
		protocol.SessionSummary{ID: "d", Slug: "delta", State: protocol.StateFailed, RetireAt: marked},
	)
	m.retirer = fr
	return m, fr
}

func TestUC_MarkedLive_Unretires(t *testing.T) {
	m, fr := ucModel()
	m, cmd := updateKey(m, key("u"))
	require.Nil(t, cmd)
	require.Equal(t, "u", m.PendingChord)
	m, cmd = updateKey(m, key("c"))
	runCmd(cmd)
	require.Equal(t, []string{"a"}, fr.unretired)
	require.Empty(t, fr.retired)
	require.Contains(t, m.Notice, "unmarked alpha")
	require.Empty(t, m.PendingChord)
}

func TestUC_QueuedMarkedUnretires(t *testing.T) {
	m, fr := ucModel()
	m.Sessions[0].State = protocol.StateQueued
	m = typeKeys(m, "u")
	_, cmd := updateKey(m, key("c"))
	runCmd(cmd)
	require.Equal(t, []string{"a"}, fr.unretired)
}

func TestUC_NotDone_Notice(t *testing.T) {
	m, fr := ucModel()
	m.SelectedID = "b"
	m = typeKeys(m, "u")
	m, cmd := updateKey(m, key("c"))
	runCmd(cmd)
	require.Equal(t, "beta isn't done", m.Notice)
	require.Empty(t, m.Err)
	require.Empty(t, fr.unretired)
	require.Empty(t, fr.retired)
}

func TestUC_Exited_ExplainsAndTouchesNothing(t *testing.T) {
	for id, name := range map[string]string{"c": "gamma", "d": "delta"} { // unmarked done, marked failed
		m, fr := ucModel()
		m.SelectedID = id
		m = typeKeys(m, "u")
		m, cmd := updateKey(m, key("c"))
		runCmd(cmd)
		require.Equal(t, "uc: "+name+" has exited; it can't be brought back yet", m.Err)
		require.Empty(t, fr.unretired, id)
		require.Empty(t, fr.retired, id)
	}
}

func TestUC_CompletesInsteadOfArmingCC(t *testing.T) {
	m, fr := ucModel()
	m = typeKeys(m, "u")
	m, cmd := updateKey(m, key("c"))
	runCmd(cmd)
	require.Empty(t, m.PendingChord, "u then c completes uc and does not arm cc")
	require.Equal(t, []string{"a"}, fr.unretired)

	// A trailing c only arms cc.
	m, cmd = updateKey(m, key("c"))
	runCmd(cmd)
	require.Equal(t, "c", m.PendingChord)
	require.Empty(t, fr.retired)
}

func TestUC_ChordInterrupted(t *testing.T) {
	m, fr := ucModel()
	m = typeKeys(m, "u", "j")
	require.Empty(t, m.PendingChord)
	require.Equal(t, "c", m.SelectedID, "j moved on (board order: b a c d)")
	m, cmd := updateKey(m, key("c"))
	runCmd(cmd)
	require.Equal(t, "c", m.PendingChord, "the c after u j only arms cc")
	require.Empty(t, fr.unretired)
	require.Empty(t, fr.retired)
}

func TestUC_UThenUStaysArmed(t *testing.T) {
	m, fr := ucModel()
	m = typeKeys(m, "u", "u")
	require.Equal(t, "u", m.PendingChord)
	m, cmd := updateKey(m, key("c"))
	runCmd(cmd)
	require.Equal(t, []string{"a"}, fr.unretired)
	require.Empty(t, m.PendingChord)
}

func TestUC_NoSelectionIsNoop(t *testing.T) {
	m, fr := ucModel()
	m.SelectedID = ""
	m = typeKeys(m, "u")
	m, cmd := updateKey(m, key("c"))
	runCmd(cmd)
	require.Empty(t, fr.unretired)
	require.Empty(t, m.Err)
	require.Empty(t, m.PendingChord)
}

func TestUC_CommandAliases(t *testing.T) {
	m, fr := ucModel()
	m.SelectedID = "b"

	next, cmd := executeCommand(m, "uc alpha")
	runCmd(cmd)
	require.Equal(t, []string{"a"}, fr.unretired, "selector wins over the selection")
	require.Contains(t, next.Notice, "unmarked alpha")

	next, _ = executeCommand(m, "uc")
	require.Equal(t, "beta isn't done", next.Notice, "defaults to the selection")

	next, _ = executeCommand(m, "uc nope")
	require.Equal(t, "no match for nope", next.Err)

	next, _ = executeCommand(m, "uc gamma")
	require.Contains(t, next.Err, "gamma has exited")

	next, _ = executeCommand(Model{Focus: FocusBoard}, "uc")
	require.Equal(t, "uc: no session selected", next.Err)

	next, _ = executeCommand(m, "resume alpha")
	require.Contains(t, next.Err, "unknown command: resume", ":resume lands with T3")
}

func TestUC_HelpListsIt(t *testing.T) {
	plain := ansi.Strip(renderHelp(120, 60))
	require.Contains(t, plain, "uc")
	require.Contains(t, plain, ":uc [sel]")
}

func TestScreensaverClearsPendingU(t *testing.T) {
	m, now, _ := saverModel(t, saverSessions()...)
	m, _ = step(t, m, key("u"))
	require.Equal(t, "u", m.PendingChord)
	m = idleTo(t, m, now, *now, 6*time.Minute)
	require.True(t, m.saverOn)
	require.Empty(t, m.PendingChord)
	m, _ = step(t, m, key("c")) // wakes, not acted on
	m, _ = step(t, m, key("c"))
	require.Equal(t, "c", m.PendingChord, "the c after waking only arms cc: no stale u")
}
