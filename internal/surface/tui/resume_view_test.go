package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

const (
	ridA = "1a2b3c4d-1111-4222-8333-444455556666"
	ridB = "9f8e7d6c-aaaa-4bbb-8ccc-ddddeeeeffff"
)

// fakeResumer records Resume intents.
type fakeResumer struct {
	resumed []string
	reqIDs  []string
	err     error
}

func (f *fakeResumer) Resume(id, reqID string) error {
	f.resumed = append(f.resumed, id)
	f.reqIDs = append(f.reqIDs, reqID)
	return f.err
}

// resumeSessions is spanSessions with resume ids: row 1 live, row 2 done.
func resumeSessions() []protocol.SessionSummary {
	s := spanSessions()
	s[0].ResumeID = ridA
	s[1].ResumeID = ridB
	return s
}

func resumeModel(width int, detailed bool) Model {
	m := layoutModel(timesNow, width, resumeSessions()...)
	m.Detailed = detailed
	return m
}

func TestApplyPatch_ResumeIDAndExitCode(t *testing.T) {
	code := 2
	s := protocol.SessionSummary{ExitCode: &code}
	applyPatch(&s, map[string]any{"resume_id": ridA})
	require.Equal(t, ridA, s.ResumeID)
	applyPatch(&s, map[string]any{"resume_id": "7d4f3c8a-1234-4abc-89ab-cdefabcdef01"})
	require.Equal(t, "7d4f3c8a-1234-4abc-89ab-cdefabcdef01", s.ResumeID, "the latest wins")

	applyPatch(&s, map[string]any{"exit_code": nil})
	require.Nil(t, s.ExitCode, "null clears the exit code (a resumed session)")
	applyPatch(&s, map[string]any{"exit_code": float64(3)})
	require.NotNil(t, s.ExitCode)
	require.Equal(t, 3, *s.ExitCode)
}

func TestBoardResumeColumn(t *testing.T) {
	m := resumeModel(160, true)
	rows := boardRows(t, m, 160)
	require.Contains(t, rows["1"], "1a2b3c4d")
	require.Contains(t, rows["2"], "9f8e7d6c")
	require.NotContains(t, rows["2"], "9f8e7d6c-", "an 8-character prefix, not the whole id")
	// After the span, before the age.
	require.Less(t, strings.Index(rows["2"], "09:00→09:40"), strings.Index(rows["2"], "9f8e7d6c"))

	t.Run("simple view has none", func(t *testing.T) {
		out := ansi.Strip(renderBoard(resumeModel(160, false), 160, 0))
		require.NotContains(t, out, "1a2b3c4d")
	})
	t.Run("only while some row has an id", func(t *testing.T) {
		none := spanModel(160, true)
		require.Zero(t, boardLayout(none, 160).resume)
		require.Positive(t, boardLayout(m, 160).resume)
	})
	t.Run("an invalid id is not shown", func(t *testing.T) {
		bad := resumeModel(160, true)
		bad.Sessions[0].ResumeID = "-x --flag"
		bad.Sessions[1].ResumeID = ""
		require.Zero(t, boardLayout(bad, 160).resume)
	})
}

// The resume id is the first column to go on a narrow terminal: the span and
// tokens survive it.
func TestBoardLayout_ResumeDropsFirst(t *testing.T) {
	sessions := resumeSessions()
	full := layoutRows(sessions, timesNow, 160, true, true, true)
	require.Equal(t, 8, full.resume)
	require.Positive(t, full.span)
	require.Positive(t, full.tokens)
	for w := 160; w > 40; w-- {
		c := layoutRows(sessions, timesNow, w, true, true, true)
		if c.resume == 0 {
			require.Positive(t, c.span, "width %d: resume dropped before span", w)
			require.Positive(t, c.tokens, "width %d: resume dropped before tokens", w)
			return
		}
	}
	t.Fatal("resume column never dropped")
}

func TestBoardLayout_ResumeRowsFillWidthExactly(t *testing.T) {
	for _, w := range layoutWidths {
		m := resumeModel(w, true)
		cols := boardLayout(m, w)
		for _, s := range m.Sessions {
			require.Equal(t, w, ansi.StringWidth(renderRow(m, s, cols)), "width %d row %s", w, s.ID)
		}
	}
}

func TestRidCommand(t *testing.T) {
	m := resumeModel(160, true)
	m.SelectedID = "1"

	t.Run("selected row", func(t *testing.T) {
		got, cmd := executeCommand(m, "rid")
		require.Nil(t, cmd)
		require.Empty(t, got.Err)
		require.Equal(t, "dark-mode: resume id "+ridA, got.Notice)
	})
	t.Run("selector", func(t *testing.T) {
		got, _ := executeCommand(m, "rid release-notes")
		require.Equal(t, "release-notes: resume id "+ridB, got.Notice)
	})
	t.Run("no id", func(t *testing.T) {
		none := resumeModel(160, true)
		none.Sessions[0].ResumeID = ""
		none.SelectedID = "1"
		got, _ := executeCommand(none, "rid")
		require.Equal(t, "rid: dark-mode has no resume id", got.Err)
		require.Empty(t, got.Notice)
	})
	t.Run("no match", func(t *testing.T) {
		got, _ := executeCommand(m, "rid nope")
		require.Equal(t, "no match for nope", got.Err)
	})
	t.Run("in the table", func(t *testing.T) {
		c, ok := lookupCommand("rid")
		require.True(t, ok)
		require.Equal(t, ":rid [sel]", c.helpKey())
		require.NotEmpty(t, c.Desc)
	})
}

func TestFriendlyDaemonError_Resume(t *testing.T) {
	cases := []struct {
		name string
		ee   protocol.ErrorEvent
		want string
	}{
		{"clearing", protocol.ErrorEvent{ID: "resume-a", Code: protocol.ErrCodeBadState, Message: protocol.MsgClearing},
			"too late — session is already being cleared"},
		{"not resumable passes through", protocol.ErrorEvent{ID: "resume-a", Code: protocol.ErrCodeBadState, Message: "session exited and claude cannot be resumed"},
			"session exited and claude cannot be resumed"},
		{"feature off", protocol.ErrorEvent{ID: "resume-a", Code: protocol.ErrCodeBadIntent, Message: "resume is off"},
			offMessage("resume")},
		{"deleted meanwhile", protocol.ErrorEvent{ID: "resume-a", Code: protocol.ErrCodeUnknownSession, Message: "session was deleted while resuming"},
			"session was deleted while resuming"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, friendlyDaemonError(tc.ee))
		})
	}
}

func TestResumeSessionCmd(t *testing.T) {
	fr := &fakeResumer{}
	require.Nil(t, resumeSessionCmd(fr, "abc")())
	require.Equal(t, []string{"abc"}, fr.resumed)
	require.Equal(t, []string{"resume-abc"}, fr.reqIDs)

	fr = &fakeResumer{err: errors.New("boom")}
	msg, ok := resumeSessionCmd(fr, "abc")().(CmdErrMsg)
	require.True(t, ok)
	require.Equal(t, "resume", msg.Op)
}

func TestResumerOf(t *testing.T) {
	require.Nil(t, resumerOf(Model{}), "no client, no resumer")
	fr := &fakeResumer{}
	require.Equal(t, fr, resumerOf(Model{resumer: fr}))
}

// resume off is inert in the TUI: no column, :rid refuses.
func TestResumeOff_TUIIsInert(t *testing.T) {
	m := resumeModel(160, true)
	m.Store = flagOff(t, "resume")
	require.Zero(t, boardLayout(m, 160).resume)
	require.NotContains(t, ansi.Strip(renderBoard(m, 160, 0)), "1a2b3c4d")

	got, cmd := executeCommand(m, "rid")
	require.Equal(t, "off: enable in Settings → Features (rex config set resume true)", got.Err)
	require.Nil(t, cmd)
	require.Empty(t, got.Notice)
}
