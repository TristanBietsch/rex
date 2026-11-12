package tui

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

type undoCallRec struct {
	intent  string
	payload any
}

// fakeUndoer answers each call from reply and records it.
type fakeUndoer struct {
	calls []undoCallRec
	reply func(intent string, payload any) (protocol.UndoResult, error)
}

func (f *fakeUndoer) Do(_ context.Context, intent string, payload any) (protocol.Envelope, error) {
	f.calls = append(f.calls, undoCallRec{intent, payload})
	res, err := f.reply(intent, payload)
	if err != nil {
		return protocol.Envelope{}, err
	}
	data, _ := json.Marshal(res)
	return protocol.Envelope{Type: protocol.EventUndoResult, Data: data}, nil
}

func badState(msg string) error {
	return &client.Error{Intent: "Undo", Code: protocol.ErrCodeBadState, Message: msg}
}

var undoPlan = protocol.UndoResult{
	Restored: []string{"internal/api/handler.go", "internal/api/handler_test.go", "README.md"},
	Deleted:  []string{"scratch/notes.txt"},
}

func undoBoardModel(t *testing.T, fu *fakeUndoer) Model {
	t.Helper()
	m := goldenBoard(t, goldenSize{120, 40}, false)
	m.undoer = fu
	require.NoError(t, m.Store.Set("turn_snapshots", true))
	m.SelectedID = "3" // perf-audit, a working session in the fixture; the daemon would refuse
	return m
}

// okUndoer plans undoPlan and, for the real call, returns it with warnings.
func okUndoer(warnings ...string) *fakeUndoer {
	return &fakeUndoer{reply: func(string, any) (protocol.UndoResult, error) {
		return protocol.UndoResult{Restored: undoPlan.Restored, Deleted: undoPlan.Deleted, Safety: "abc", Warnings: warnings}, nil
	}}
}

// squash drops the box drawing and every space, so a message the dialog
// wrapped still matches the text it was wrapped from.
func squash(s string) string {
	return strings.Join(strings.Fields(strings.NewReplacer("│", " ").Replace(ansi.Strip(s))), "")
}

func undoOpen(t *testing.T, m Model, line string) (Model, tea.Cmd) {
	t.Helper()
	m, cmd := executeCommand(m, line)
	return m, cmd
}

func TestUndo_DryRunThenConfirmThenResult(t *testing.T) {
	fu := okUndoer("the folder changed while the restore ran: 1 file differs")
	m := undoBoardModel(t, fu)
	m, cmd := undoOpen(t, m, "undo")
	require.Equal(t, FocusUndo, m.Focus)
	require.Contains(t, ansi.Strip(m.View()), "checking what would change")
	m = deliver(t, m, cmd)

	require.Len(t, fu.calls, 1)
	require.Equal(t, protocol.IntentUndo, fu.calls[0].intent)
	require.Equal(t, protocol.Undo{SessionID: "3", Turns: 1, DryRun: true}, fu.calls[0].payload, "the dry run comes first")
	out := ansi.Strip(m.View())
	for _, want := range []string{"UNDO", "restore 3 files, delete 1 file", "internal/api/handler.go", "scratch/notes.txt",
		shellEffectsNote, "y undo", "esc cancel"} {
		require.Contains(t, squash(renderUndo(m)), squash(want))
	}
	require.NotContains(t, out, "force", "no force option without a shared folder")

	m, cmd = updateKey(m, key("y"))
	m = deliver(t, m, cmd)
	require.Len(t, fu.calls, 2)
	require.Equal(t, protocol.Undo{SessionID: "3", Turns: 1}, fu.calls[1].payload, "the real call has no dry run")
	out = ansi.Strip(m.View())
	require.Contains(t, out, "3 files restored, 1 deleted")
	require.Contains(t, squash(renderUndo(m)), squash("! the folder changed while the restore ran: 1 file differs"), "warnings are shown after the run")
	require.Contains(t, out, ":redo brings the changes back")
	require.Contains(t, out, "The agent was not told")

	m, _ = updateKey(m, key("esc"))
	require.Equal(t, FocusBoard, m.Focus)
	require.Nil(t, m.Undo)
}

func TestUndo_NCancelsWithoutTheRealCall(t *testing.T) {
	fu := okUndoer()
	m := undoBoardModel(t, fu)
	m, cmd := undoOpen(t, m, "undo")
	m = deliver(t, m, cmd)
	m, cmd = updateKey(m, key("n"))
	require.Nil(t, cmd)
	require.Equal(t, FocusBoard, m.Focus)
	require.Len(t, fu.calls, 1, "only the dry run")
}

func TestUndo_TurnsAndSelector(t *testing.T) {
	fu := okUndoer()
	m := undoBoardModel(t, fu)
	m, cmd := undoOpen(t, m, "undo release-notes -turns 3")
	m = deliver(t, m, cmd)
	require.Equal(t, protocol.Undo{SessionID: "2", Turns: 3, DryRun: true}, fu.calls[0].payload)
	require.Contains(t, ansi.Strip(m.View()), "release-notes · 3 turns")

	for _, bad := range []string{"undo -turns 0", "undo -turns x", "undo -turns", "undo -turns 1001"} {
		m2, cmd := undoOpen(t, undoBoardModel(t, okUndoer()), bad)
		require.Nil(t, cmd, bad)
		require.Equal(t, FocusBoard, m2.Focus)
		require.Equal(t, "undo: -turns must be 1 to 1000", m2.Err, bad)
	}
	m2, _ := undoOpen(t, undoBoardModel(t, okUndoer()), "undo nope")
	require.Equal(t, "no match for nope", m2.Err)
	m2, _ = undoOpen(t, undoBoardModel(t, okUndoer()), "undo a b")
	require.Equal(t, "undo: at most one session", m2.Err)
}

func TestRedo_UsesTheRedoIntent(t *testing.T) {
	fu := okUndoer()
	m := undoBoardModel(t, fu)
	m, cmd := undoOpen(t, m, "redo")
	m = deliver(t, m, cmd)
	require.Equal(t, protocol.IntentRedo, fu.calls[0].intent)
	require.Equal(t, protocol.Redo{SessionID: "3", DryRun: true}, fu.calls[0].payload)
	require.Contains(t, ansi.Strip(m.View()), "REDO")
	require.Contains(t, ansi.Strip(m.View()), "y redo")
	m, cmd = updateKey(m, key("y"))
	m = deliver(t, m, cmd)
	require.Equal(t, protocol.Redo{SessionID: "3"}, fu.calls[1].payload)
	require.NotContains(t, ansi.Strip(m.View()), "The agent was not told", "that note is for undo")

	m2, cmd := undoOpen(t, undoBoardModel(t, okUndoer()), "redo -turns 2")
	require.Nil(t, cmd)
	require.Contains(t, m2.Err, "redo: no -turns")
}

// The daemon's refusals reach the status line as they are, and no dialog
// stays open.
func TestUndo_RefusalsAreNotices(t *testing.T) {
	cases := map[string]string{
		"working":      "session is working: wait until the agent stops, or press esc to interrupt it first",
		"no git repo":  "snapshots need a git repo; this folder is not inside one",
		"nothing":      "nothing to undo",
		"blocked":      "blocked paths: vendor/x is now a directory",
		"nothing redo": "nothing to redo",
	}
	for name, msg := range cases {
		t.Run(name, func(t *testing.T) {
			fu := &fakeUndoer{reply: func(string, any) (protocol.UndoResult, error) { return protocol.UndoResult{}, badState(msg) }}
			m := undoBoardModel(t, fu)
			m, cmd := undoOpen(t, m, "undo")
			m = deliver(t, m, cmd)
			require.Equal(t, FocusBoard, m.Focus)
			require.Nil(t, m.Undo)
			require.Equal(t, "undo: "+msg, m.Err)
			require.Len(t, fu.calls, 1)
		})
	}
	t.Run("unknown session", func(t *testing.T) {
		fu := &fakeUndoer{reply: func(string, any) (protocol.UndoResult, error) {
			return protocol.UndoResult{}, &client.Error{Code: protocol.ErrCodeUnknownSession, Message: "session not found"}
		}}
		m, cmd := undoOpen(t, undoBoardModel(t, fu), "undo")
		m = deliver(t, m, cmd)
		require.Equal(t, "undo: session is gone", m.Err)
	})
	t.Run("daemon says off", func(t *testing.T) {
		fu := &fakeUndoer{reply: func(string, any) (protocol.UndoResult, error) {
			return protocol.UndoResult{}, &client.Error{Code: protocol.ErrCodeBadIntent, Message: "off: enable turn_snapshots in Settings → Features"}
		}}
		m, cmd := undoOpen(t, undoBoardModel(t, fu), "undo")
		m = deliver(t, m, cmd)
		require.Equal(t, "off: enable in Settings → Features (rex config set turn_snapshots true)", m.Err)
	})
}

const sharedMsg = "another live session works in this folder (api-refactor, docs); its changes would be reverted too. Use force to go ahead"

// sharedUndoer refuses with the shared-folder message unless forced.
func sharedUndoer() *fakeUndoer {
	return &fakeUndoer{reply: func(intent string, payload any) (protocol.UndoResult, error) {
		force := false
		switch p := payload.(type) {
		case protocol.Undo:
			force = p.Force
		case protocol.Redo:
			force = p.Force
		}
		if !force {
			return protocol.UndoResult{}, badState(sharedMsg)
		}
		return protocol.UndoResult{Restored: undoPlan.Restored, Deleted: undoPlan.Deleted}, nil
	}}
}

func TestUndo_SharedFolderOffersForce(t *testing.T) {
	fu := sharedUndoer()
	m := undoBoardModel(t, fu)
	m, cmd := undoOpen(t, m, "undo")
	m = deliver(t, m, cmd)
	require.Equal(t, FocusUndo, m.Focus, "the shared-folder refusal opens the dialog, not a notice")
	out := ansi.Strip(m.View())
	require.Contains(t, squash(renderUndo(m)), squash("another live session works in this folder (api-refactor, docs)"))
	require.Contains(t, out, "f force")

	m, cmd = updateKey(m, key("y"))
	require.Nil(t, cmd, "y does nothing before the force choice")
	require.Len(t, fu.calls, 1)

	m, cmd = updateKey(m, key("f"))
	m = deliver(t, m, cmd)
	require.Len(t, fu.calls, 2)
	require.Equal(t, protocol.Undo{SessionID: "3", Turns: 1, Force: true, DryRun: true}, fu.calls[1].payload)
	out = ansi.Strip(m.View())
	require.Contains(t, out, "restore 3 files, delete 1 file", "the forced plan")
	require.Contains(t, squash(renderUndo(m)), squash("forced: the other live session in this folder is reverted too (api-refactor, docs)"))

	m, cmd = updateKey(m, key("y"))
	m = deliver(t, m, cmd)
	require.Equal(t, protocol.Undo{SessionID: "3", Turns: 1, Force: true}, fu.calls[2].payload, "the real call carries force")
	require.Contains(t, ansi.Strip(m.View()), "3 files restored")
}

func TestUndo_ForceIsOnlyForTheSharedFolderCase(t *testing.T) {
	fu := &fakeUndoer{reply: func(string, any) (protocol.UndoResult, error) { return protocol.UndoResult{}, badState("session is working: wait") }}
	m := undoBoardModel(t, fu)
	m, cmd := undoOpen(t, m, "undo")
	m = deliver(t, m, cmd)
	m, cmd = updateKey(m, key("f"))
	require.Nil(t, cmd)
	require.Len(t, fu.calls, 1)
	require.Equal(t, FocusBoard, m.Focus)
	require.NotContains(t, ansi.Strip(m.View()), "force")
}

func TestUndo_ForcedStillRefusedByTheDaemon(t *testing.T) {
	// The guard can trip after force (a second session started meanwhile):
	// the refusal is a notice, never a loop of force prompts.
	n := 0
	fu := &fakeUndoer{reply: func(string, any) (protocol.UndoResult, error) {
		n++
		if n == 1 {
			return protocol.UndoResult{}, badState(sharedMsg)
		}
		return protocol.UndoResult{}, badState(sharedMsg)
	}}
	m := undoBoardModel(t, fu)
	m, cmd := undoOpen(t, m, "undo")
	m = deliver(t, m, cmd)
	m, cmd = updateKey(m, key("f"))
	m = deliver(t, m, cmd)
	require.Equal(t, FocusBoard, m.Focus)
	require.Contains(t, m.Err, "another live session")
}

func TestUndo_RunFailureStaysInTheDialog(t *testing.T) {
	calls := 0
	fu := &fakeUndoer{reply: func(string, any) (protocol.UndoResult, error) {
		calls++
		if calls == 1 {
			return protocol.UndoResult{Restored: []string{"a.go"}}, nil
		}
		return protocol.UndoResult{}, badState("undo stopped half way; rex redo brings back what was there")
	}}
	m := undoBoardModel(t, fu)
	m, cmd := undoOpen(t, m, "undo")
	m = deliver(t, m, cmd)
	m, cmd = updateKey(m, key("y"))
	m = deliver(t, m, cmd)
	require.Equal(t, FocusUndo, m.Focus)
	out := ansi.Strip(m.View())
	require.Contains(t, squash(renderUndo(m)), squash("undo stopped half way"))
	require.Contains(t, out, "The folder may be part way")
}

func TestUndo_OffIsInert(t *testing.T) {
	fu := okUndoer()
	m := undoBoardModel(t, fu)
	m.Store = settings.NewStore() // turn_snapshots defaults to off
	require.False(t, m.featureOn("turn_snapshots"))
	for _, line := range []string{"undo", "redo", "undo -turns 2"} {
		m2, cmd := executeCommand(m, line)
		require.Nil(t, cmd, line)
		require.Equal(t, FocusBoard, m2.Focus)
		require.Equal(t, "off: enable in Settings → Features (rex config set turn_snapshots true)", m2.Err)
	}
	require.Empty(t, fu.calls, "nothing is sent while the feature is off")
}

func TestUndo_StaleRepliesAreDropped(t *testing.T) {
	fu := okUndoer()
	m := undoBoardModel(t, fu)
	m, cmd := undoOpen(t, m, "undo")
	late := cmd()
	m, _ = updateKey(m, key("esc")) // let go of the dry run
	require.Equal(t, FocusBoard, m.Focus)
	next, _ := m.Update(late)
	require.Nil(t, next.(Model).Undo)
	require.Equal(t, FocusBoard, next.(Model).Focus)
}

func TestUndo_NotConnectedAndNoSelection(t *testing.T) {
	m := goldenBoard(t, goldenSize{120, 40}, false)
	require.NoError(t, m.Store.Set("turn_snapshots", true))
	m.SelectedID = "3"
	m, cmd := executeCommand(m, "undo")
	require.Nil(t, cmd)
	require.Equal(t, "undo: not connected to the daemon", m.Err)

	m = undoBoardModel(t, okUndoer())
	m.SelectedID = ""
	m, cmd = executeCommand(m, "undo")
	require.Nil(t, cmd)
	require.Equal(t, "undo: no session selected", m.Err)
}

func TestUndo_LongListsAreCut(t *testing.T) {
	fu := &fakeUndoer{reply: func(string, any) (protocol.UndoResult, error) {
		var r []string
		for i := 0; i < 30; i++ {
			r = append(r, "pkg/file"+strings.Repeat("x", 80)+".go")
		}
		return protocol.UndoResult{Restored: r}, nil
	}}
	m := undoBoardModel(t, fu)
	m, cmd := undoOpen(t, m, "undo")
	m = deliver(t, m, cmd)
	out := ansi.Strip(m.View())
	require.Contains(t, out, "restore 30 files, delete 0 files")
	require.Contains(t, out, "… and 24 more")
	for _, l := range strings.Split(out, "\n") {
		require.LessOrEqual(t, ansi.StringWidth(l), 120)
	}
	require.Len(t, strings.Split(out, "\n"), 40)
}

// File names and messages come from the daemon and can hold anything.
func TestUndo_UntrustedText(t *testing.T) {
	fu := &fakeUndoer{reply: func(string, any) (protocol.UndoResult, error) {
		return protocol.UndoResult{Restored: []string{"a\x1b[31m\nb.go"}, Warnings: []string{"w\x1b[2Jarn"}}, nil
	}}
	m := undoBoardModel(t, fu)
	m, cmd := undoOpen(t, m, "undo")
	m = deliver(t, m, cmd)
	require.NotContains(t, m.View(), "\x1b[31m")
	m, cmd = updateKey(m, key("y"))
	m = deliver(t, m, cmd)
	require.NotContains(t, m.View(), "\x1b[2J")
}

// undoDialogModel is the golden fixture: the dialog in a given phase.
func undoDialogModel(t *testing.T, sz goldenSize, phase string) Model {
	t.Helper()
	fu := okUndoer("the folder changed while the restore ran: 1 file differs")
	if phase == "shared" || phase == "forced" {
		fu = sharedUndoer()
	}
	m := goldenBoard(t, sz, false)
	m.undoer = fu
	require.NoError(t, m.Store.Set("turn_snapshots", true))
	m.SelectedID = "2"
	m, cmd := executeCommand(m, "undo")
	m = deliver(t, m, cmd)
	switch phase {
	case "done":
		m, cmd = updateKey(m, key("y"))
		m = deliver(t, m, cmd)
	case "forced":
		m, cmd = updateKey(m, key("f"))
		m = deliver(t, m, cmd)
	}
	return m
}
