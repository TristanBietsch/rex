package tui

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/catalog/theme"
	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

const (
	loopA = "loop-0000000a"
	loopB = "loop-0000000b"
)

// recLoops records the loop intents and answers with a canned loop.
type recLoops struct {
	calls []string
	reply protocol.LoopSummary
	err   error
}

func (r *recLoops) PauseLoop(_ context.Context, id string) (protocol.LoopSummary, error) {
	r.calls = append(r.calls, "pause "+id)
	return r.reply, r.err
}

func (r *recLoops) ResumeLoop(_ context.Context, id string) (protocol.LoopSummary, error) {
	r.calls = append(r.calls, "resume "+id)
	return r.reply, r.err
}

func (r *recLoops) StopLoop(_ context.Context, id string) (protocol.LoopSummary, error) {
	r.calls = append(r.calls, "stop "+id)
	return r.reply, r.err
}

func exit(n int) *int { return &n }

func testLoops() []protocol.LoopSummary {
	return []protocol.LoopSummary{
		{ID: loopA, Name: "fix-until-green", Kind: "repeat", State: protocol.LoopRunning, SessionID: "1",
			Iteration: 3, MaxIterations: 8, MaxMinutes: 120, ElapsedSeconds: 12 * 60, LastCheckExit: exit(1),
			StartedAt: goldenNow.Add(-30 * time.Minute), UpdatedAt: goldenNow},
		{ID: loopB, Name: "tidy-docs", Kind: "repeat", State: protocol.LoopPaused, PauseReason: protocol.PauseNeedsInput, SessionID: "2",
			Iteration: 2, MaxIterations: 5, MaxMinutes: 60, ElapsedSeconds: 5 * 60,
			StartedAt: goldenNow.Add(-2 * time.Hour), UpdatedAt: goldenNow.Add(-time.Hour)},
	}
}

func loopSessions() []protocol.SessionSummary {
	return []protocol.SessionSummary{
		{ID: "1", ShortID: "aa11", ToolID: "claude", ModelID: "opus", Slug: "fixer", Title: "make check green",
			State: protocol.StateWorking, LoopID: loopA, LoopIter: 3},
		{ID: "2", ShortID: "bb22", ToolID: "claude", ModelID: "opus", Slug: "docs", Title: "tidy the docs",
			State: protocol.StateNeedsInput, LoopID: loopB, LoopIter: 2},
		{ID: "3", ShortID: "cc33", ToolID: "claude", ModelID: "opus", Slug: "plain", Title: "no loop here",
			State: protocol.StateWorking},
	}
}

func loopModel(t *testing.T) Model {
	t.Helper()
	m := layoutModel(goldenNow, 120, loopSessions()...)
	m.Detailed = false
	m.SelectedID = "1"
	m.Loops = testLoops()
	require.NoError(t, m.Store.Set("loops", true))
	return m
}

func TestLoopChip_StatesAndOff(t *testing.T) {
	m := loopModel(t)
	s := m.Sessions
	require.Equal(t, "⟳ 3/8 running", loopChip(m, s[0]))
	require.Equal(t, "⟳ 2/5 paused: needs you", loopChip(m, s[1]))
	require.Empty(t, loopChip(m, s[2]), "an ordinary session has no chip")

	for _, tc := range []struct {
		l    protocol.LoopSummary
		want string
	}{
		{protocol.LoopSummary{State: protocol.LoopDone, StopReason: protocol.StopCheckPassed}, "done"},
		{protocol.LoopSummary{State: protocol.LoopCapped, StopReason: protocol.StopMaxIterations}, "capped: max iterations"},
		{protocol.LoopSummary{State: protocol.LoopCapped, StopReason: protocol.StopMaxMinutes}, "capped: max minutes"},
		{protocol.LoopSummary{State: protocol.LoopFailed, StopReason: protocol.StopCheckError}, "failed"},
		{protocol.LoopSummary{State: protocol.LoopStopped}, "stopped"},
		{protocol.LoopSummary{State: protocol.LoopPaused, PauseReason: protocol.PauseUser}, "paused"},
		{protocol.LoopSummary{State: protocol.LoopPaused, PauseReason: "made-up\x1b[31m"}, "paused"},
		{protocol.LoopSummary{State: "weird"}, "unknown"},
	} {
		require.Equal(t, tc.want, loopStateText(tc.l), tc.l.State)
	}

	// The daemon no longer lists the loop: the turn alone.
	m.Loops = nil
	require.Equal(t, "⟳ 3", loopChip(m, s[0]))

	// Off: no marker at all.
	m = loopModel(t)
	require.NoError(t, m.Store.Set("loops", false))
	require.Empty(t, loopChip(m, s[0]))
	require.NotContains(t, ansi.Strip(renderBoard(m, 120, 0)), "⟳")
}

func TestLoopChip_ThemeGlyph(t *testing.T) {
	t.Cleanup(func() { curGlyphs = theme.Glyphs{} })
	curGlyphs = theme.Glyphs{Loop: "@"}
	m := loopModel(t)
	require.Equal(t, "@ 3/8 running", loopChip(m, m.Sessions[0]))
	require.Equal(t, 1, ansi.StringWidth(loopGlyph()))
	curGlyphs = theme.Glyphs{}
	require.Equal(t, 1, ansi.StringWidth(loopGlyph()), "the built-in marker is one cell")
}

func TestLoopRows_ShowChipInEveryLayout(t *testing.T) {
	m := loopModel(t)
	out := ansi.Strip(renderBoard(m, 120, 0))
	require.Contains(t, out, "⟳ 3/8 running · make check green")
	require.Contains(t, out, "⟳ 2/5 paused: needs you")
	require.Contains(t, out, "no loop here")

	m.Detailed = true
	require.Contains(t, ansi.Strip(renderBoard(m, 160, 0)), "⟳ 3/8 running")

	m.Detailed = false
	require.NoError(t, m.Store.Set("mobile_layout", "always"))
	require.Contains(t, ansi.Strip(renderBoard(m, 50, 0)), "⟳ 3/8")
}

func TestLoopKey_PauseResumeToggles(t *testing.T) {
	rec := &recLoops{}
	m := loopModel(t)
	m.looper = rec

	rec.reply = testLoops()[0]
	rec.reply.State = protocol.LoopPaused
	rec.reply.PauseReason = protocol.PauseUser
	m, cmd := updateKey(m, key("p"))
	require.NotNil(t, cmd)
	msg := cmd().(loopActionMsg)
	require.Equal(t, []string{"pause " + loopA}, rec.calls)
	nm, _ := m.Update(msg)
	m = nm.(Model)
	require.Equal(t, "loop fix-until-green: paused", m.Notice)
	l, _ := loopByID(m, loopA)
	require.Equal(t, protocol.LoopPaused, l.State)

	// On a paused loop p resumes.
	rec.reply.State = protocol.LoopRunning
	rec.reply.PauseReason = ""
	m, cmd = updateKey(m, key("p"))
	require.NotNil(t, cmd)
	cmd()
	require.Equal(t, []string{"pause " + loopA, "resume " + loopA}, rec.calls)

	// A paused-by-needs-input loop resumes with p too.
	m.SelectedID = "2"
	_, cmd = updateKey(m, key("p"))
	require.NotNil(t, cmd)
	cmd()
	require.Equal(t, "resume "+loopB, rec.calls[len(rec.calls)-1])
}

func TestLoopKey_NotALoopRow(t *testing.T) {
	rec := &recLoops{}
	m := loopModel(t)
	m.looper = rec
	m.SelectedID = "3"
	for _, k := range []string{"p", "x"} {
		m2, cmd := updateKey(m, key(k))
		require.Nil(t, cmd)
		require.Contains(t, m2.Err, "is not a loop session", k)
	}
	require.Empty(t, rec.calls)
}

func TestLoopKey_StopAsksFirst(t *testing.T) {
	rec := &recLoops{reply: testLoops()[0]}
	rec.reply.State = protocol.LoopStopped
	m := loopModel(t)
	m.looper = rec

	m, cmd := updateKey(m, key("x"))
	require.Nil(t, cmd, "nothing is sent before the answer")
	require.Equal(t, FocusConfirmLoopStop, m.Focus)
	require.Equal(t, loopA, m.PendingLoopStop)
	bar := ansi.Strip(renderLoopStopConfirm(m, 120))
	require.Contains(t, bar, "stop loop fix-until-green (3/8)?")
	require.Contains(t, bar, "y / N")
	require.Contains(t, ansi.Strip(m.View()), "stop loop fix-until-green")

	// Anything but y keeps it running.
	m2, cmd := updateKey(m, key("n"))
	require.Nil(t, cmd)
	require.Equal(t, FocusBoard, m2.Focus)
	require.Empty(t, rec.calls)
	m2, _ = updateKey(m, key("esc"))
	require.Equal(t, FocusBoard, m2.Focus)
	require.Empty(t, rec.calls)

	// y stops it.
	m, cmd = updateKey(m, key("y"))
	require.Equal(t, FocusBoard, m.Focus)
	require.NotNil(t, cmd)
	cmd()
	require.Equal(t, []string{"stop " + loopA}, rec.calls)
}

func TestLoopKey_PauseNeedsNoConfirm(t *testing.T) {
	rec := &recLoops{reply: testLoops()[0]}
	m := loopModel(t)
	m.looper = rec
	m, cmd := updateKey(m, key("p"))
	require.Equal(t, FocusBoard, m.Focus)
	require.NotNil(t, cmd)
}

func TestLoopKeys_EndedLoopOnlyNotices(t *testing.T) {
	rec := &recLoops{}
	m := loopModel(t)
	m.looper = rec
	m.Loops[0].State = protocol.LoopDone
	for _, k := range []string{"p", "x"} {
		m2, cmd := updateKey(m, key(k))
		require.Nil(t, cmd, k)
		require.Equal(t, FocusBoard, m2.Focus)
		require.Contains(t, m2.Notice, "already ended (done)")
	}
	require.Empty(t, rec.calls)
}

func TestLoopCommands(t *testing.T) {
	rec := &recLoops{reply: testLoops()[0]}
	m := loopModel(t)
	m.looper = rec

	m2, cmd := executeCommand(m, "loop")
	require.Nil(t, cmd)
	require.Contains(t, m2.Err, "use :loop pause|resume|stop")
	m2, _ = executeCommand(m, "loop bogus")
	require.Contains(t, m2.Err, "use :loop pause|resume|stop")

	m2, cmd = executeCommand(m, "loop pause")
	require.Empty(t, m2.Err)
	require.NotNil(t, cmd)
	cmd()
	require.Equal(t, "pause "+loopA, rec.calls[0])

	// With a selector: the loop of another session.
	m2, cmd = executeCommand(m, "loop resume docs")
	require.NotNil(t, cmd)
	cmd()
	require.Equal(t, "resume "+loopB, rec.calls[1])

	// stop asks first, from the command too.
	m2, cmd = executeCommand(m, "loop stop")
	require.Nil(t, cmd)
	require.Equal(t, FocusConfirmLoopStop, m2.Focus)

	m2, _ = executeCommand(m, "loop pause plain")
	require.Contains(t, m2.Err, "not a loop session")
	m2, _ = executeCommand(m, "loop pause nosuch")
	require.Contains(t, m2.Err, "no match")

	m2, _ = executeCommand(m, "loops")
	require.Equal(t, FocusLoops, m2.Focus)
	m2, _ = executeCommand(m2, "loops")
	require.Equal(t, FocusBoard, m2.Focus, "a second :loops closes it")
}

// loops off is inert: no marker, keys and commands answer the off message.
func TestLoopsOff_IsInert(t *testing.T) {
	rec := &recLoops{}
	m := loopModel(t)
	m.looper = rec
	require.NoError(t, m.Store.Set("loops", false))
	off := offMessage("loops")
	require.Equal(t, "off: enable in Settings → Features (rex config set loops true)", off)

	for _, k := range []string{"p", "x", "L"} {
		m2, cmd := updateKey(m, key(k))
		require.Nil(t, cmd, k)
		require.Equal(t, off, m2.Err, k)
		require.Equal(t, FocusBoard, m2.Focus, k)
	}
	for _, line := range []string{"loop pause", "loop stop", "loops"} {
		m2, cmd := executeCommand(m, line)
		require.Nil(t, cmd, line)
		require.Equal(t, off, m2.Err, line)
		require.Equal(t, FocusBoard, m2.Focus, line)
	}
	require.Empty(t, rec.calls)
	require.Empty(t, loopChip(m, m.Sessions[0]))
}

func TestLoopsOff_HelpRowsAreMarked(t *testing.T) {
	m := loopModel(t)
	require.NoError(t, m.Store.Set("loops", false))
	marked := map[string]bool{}
	for _, g := range markOff(m, append(helpLeft(m), helpRight...)) {
		for _, r := range g.Rows {
			if r.Off {
				marked[r.Key] = true
			}
		}
	}
	for _, k := range []string{"p", "x", "L", ":loop pause|resume|stop [sel]", ":loops"} {
		require.True(t, marked[k], "help row %q is flagged off", k)
	}
}

func TestLoopAction_ErrorsShowOnTheStatusLine(t *testing.T) {
	m := loopModel(t)
	m = handleLoopAction(m, loopActionMsg{Op: "pause", Err: &client.Error{Intent: protocol.IntentPauseLoop,
		Code: protocol.ErrCodeBadIntent, Message: "intent not implemented"}})
	require.Contains(t, m.Err, "loop pause: ")
	require.Contains(t, m.Err, "rex daemon restart", "an old daemon says to update")

	m = handleLoopAction(m, loopActionMsg{Op: "stop", Err: errors.New("loop already ended")})
	require.Equal(t, "loop stop: loop already ended", m.Err)
}

func TestLoopEvents_SnapshotEventAndPatch(t *testing.T) {
	m := loopModel(t)
	m.Loops = nil
	m = adoptLoops(m, &protocol.Snapshot{Loops: testLoops()})
	require.Len(t, m.Loops, 2)

	upd := testLoops()[0]
	upd.State = protocol.LoopDone
	upd.StopReason = protocol.StopCheckPassed
	raw, err := json.Marshal(upd)
	require.NoError(t, err)
	m = m.applyEvent(protocol.Envelope{Type: protocol.EventLoop, Data: raw})
	require.Len(t, m.Loops, 2, "same id replaces")
	l, _ := loopByID(m, loopA)
	require.Equal(t, protocol.LoopDone, l.State)

	bad, err := json.Marshal(protocol.LoopSummary{ID: "../../x", Name: "evil"})
	require.NoError(t, err)
	m = m.applyEvent(protocol.Envelope{Type: protocol.EventLoop, Data: bad})
	require.Len(t, m.Loops, 2, "an id that is not ours is dropped")

	third := protocol.LoopSummary{ID: "loop-0000000c", Name: "new", State: protocol.LoopRunning}
	raw, _ = json.Marshal(third)
	m = m.applyEvent(protocol.Envelope{Type: protocol.EventLoop, Data: raw})
	require.Len(t, m.Loops, 3)

	// SetLoop's patch moves a session's turn.
	s := protocol.SessionSummary{ID: "x"}
	applyPatch(&s, map[string]any{"loop_id": loopA, "loop_iter": float64(4)})
	require.Equal(t, loopA, s.LoopID)
	require.Equal(t, 4, s.LoopIter)
}

func TestLoopsOverlay_ListsStateTurnTimeAndCheck(t *testing.T) {
	m := loopModel(t)
	m.Loops[0].UpdatedAt = goldenNow.Add(-3 * time.Minute) // running: 12m + 3m since
	m, _ = executeCommand(m, "loops")
	out := ansi.Strip(renderLoopsOverlay(m))
	require.Contains(t, out, "Loops")
	require.Contains(t, out, "fix-until-green")
	require.Contains(t, out, "running")
	require.Contains(t, out, "3/8")
	require.Contains(t, out, "15m/120m", "running time so far over max_minutes")
	require.Contains(t, out, "check exit 1")
	require.Contains(t, out, "tidy-docs")
	require.Contains(t, out, "paused: needs you")
	require.Contains(t, out, "5m/60m", "a paused loop's clock is frozen")
	require.Contains(t, out, "no check yet")
	require.Less(t, strings.Index(out, "fix-until-green"), strings.Index(out, "tidy-docs"))

	// Finished loops sort after live ones.
	m.Loops = append(m.Loops, protocol.LoopSummary{ID: "loop-0000000c", Name: "old-one", State: protocol.LoopDone,
		StartedAt: goldenNow, LastCheckExit: exit(0), Iteration: 2, MaxIterations: 8})
	out = ansi.Strip(renderLoopsOverlay(m))
	require.Less(t, strings.Index(out, "tidy-docs"), strings.Index(out, "old-one"))
	require.Contains(t, out, "check passed")
}

func TestLoopsOverlay_KeysAndEmpty(t *testing.T) {
	rec := &recLoops{reply: testLoops()[0]}
	m := loopModel(t)
	m.looper = rec
	m, _ = updateKey(m, key("L"))
	require.Equal(t, FocusLoops, m.Focus)

	m, _ = updateKey(m, key("j"))
	require.Equal(t, 1, m.LoopsSel)
	m, _ = updateKey(m, key("j"))
	require.Equal(t, 1, m.LoopsSel, "clamped")
	m, _ = updateKey(m, key("k"))
	require.Equal(t, 0, m.LoopsSel)

	// p acts on the selected loop, x asks and returns to the overlay.
	_, cmd := updateKey(m, key("p"))
	require.NotNil(t, cmd)
	cmd()
	require.Equal(t, "pause "+loopA, rec.calls[0])
	m2, cmd := updateKey(m, key("x"))
	require.Nil(t, cmd)
	require.Equal(t, FocusConfirmLoopStop, m2.Focus)
	m2, _ = updateKey(m2, key("n"))
	require.Equal(t, FocusLoops, m2.Focus, "N goes back to the overlay")

	// enter selects the loop's session on the board.
	m.LoopsSel = 1
	m, _ = updateKey(m, key("enter"))
	require.Equal(t, FocusBoard, m.Focus)
	require.Equal(t, "2", m.SelectedID)

	m, _ = updateKey(m, key("L"))
	m, _ = updateKey(m, key("esc"))
	require.Equal(t, FocusBoard, m.Focus)

	m.Loops = nil
	require.Contains(t, ansi.Strip(renderLoopsOverlay(m)), "No loops.")
}

func TestLoopsOverlay_SanitizesDaemonText(t *testing.T) {
	m := loopModel(t)
	m.Loops[0].Name = "evil\x1b[31m\nname"
	m.Loops[0].Notice = "capped\x1b]0;x\x07 now"
	out := renderLoopsOverlay(m)
	require.NotContains(t, out, "\x1b[31m")
	require.NotContains(t, out, "\x1b]0")
}

func TestLoopStopConfirm_FromTheOverlayOnAnUnlistedLoop(t *testing.T) {
	m := loopModel(t)
	m.PendingLoopStop = "loop-000000ff"
	m.Focus = FocusConfirmLoopStop
	m, cmd := updateKey(m, key("y"))
	require.Nil(t, cmd)
	require.Contains(t, m.Err, "the loop is gone")
}

func TestLoopsFlagRegistered(t *testing.T) {
	_, ok := settings.Find(loopsFlag)
	require.True(t, ok)
	require.False(t, settings.FeatureOn(settings.NewStore(), loopsFlag), "off by default")
}
