package tui

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// questionCases are LastLine edge cases with what a question check finds.
var questionCases = []struct {
	name, line, want string
}{
	{"plain question", "Proceed?", "Proceed?"},
	{"trailing space after ?", "Proceed?  ", "Proceed?"},
	{"space before ?", "Delete it ?", "Delete it ?"},
	{"multi-line, question last", "Plan ready.\nApply it?", "Plan ready. Apply it?"},
	{"multi-line, question first", "Apply it?\nWaiting for input", ""},
	{"no question", "Working…", ""},
	{"empty", "", ""},
}

// TestRowQuestion pins rowQuestion: a Ready row's flattened, trimmed last
// line ending in ?, never for other states or marked-done rows.
func TestRowQuestion(t *testing.T) {
	for _, c := range questionCases {
		s := protocol.SessionSummary{State: protocol.StateNeedsInput, LastLine: c.line}
		require.Equal(t, c.want, rowQuestion(s), c.name)
		s.State = protocol.StateWorking
		require.Empty(t, rowQuestion(s), "working: %s", c.name)
		s.State, s.RetireAt = protocol.StateNeedsInput, cellNow
		require.Empty(t, rowQuestion(s), "marked done: %s", c.name)
	}
}

// TestReasonText_Question pins the why slot's question rule and the
// WaitReason precedence: input and review win over the last line, an
// unknown reason shows nothing, marked-done rows show nothing.
func TestReasonText_Question(t *testing.T) {
	m := cellModel(t, "waiting_section", true)
	for _, c := range questionCases {
		s := protocol.SessionSummary{State: protocol.StateNeedsInput, LastLine: c.line}
		got, _ := reasonText(m, s)
		want := ""
		if c.want != "" {
			want = "? asks"
		}
		require.Equal(t, want, got, c.name)

		s.WaitReason = protocol.WaitReview
		got, _ = reasonText(m, s)
		require.Equal(t, "✓ review", got, "review wins over the line: %s", c.name)
		s.WaitReason = protocol.WaitInput
		got, _ = reasonText(m, s)
		require.Equal(t, "? asks", got, "input: %s", c.name)
		s.WaitReason = "other"
		got, _ = reasonText(m, s)
		require.Empty(t, got, "unknown reason: %s", c.name)

		s.WaitReason, s.RetireAt = "", cellNow
		got, _ = reasonText(m, s)
		require.Empty(t, got, "marked done: %s", c.name)
		s.WaitReason = protocol.WaitInput
		got, _ = reasonText(m, s)
		require.Empty(t, got, "marked done with a reason: %s", c.name)

		w := protocol.SessionSummary{State: protocol.StateWorking, LastLine: c.line}
		got, _ = reasonText(m, w)
		require.Empty(t, got, "working, not waiting: %s", c.name)
	}
}

// TestBannerParts_Question pins the banner's question lead. It differs from
// rowQuestion on purpose: the question comes after the summary and the task,
// and it is not gated on the state or on marked done.
func TestBannerParts_Question(t *testing.T) {
	m := bannerModel(t, 120, 40, bannerSession())
	for _, c := range questionCases {
		for _, st := range []protocol.State{protocol.StateNeedsInput, protocol.StateWorking, protocol.StateDone} {
			for _, marked := range []bool{false, true} {
				s := protocol.SessionSummary{ID: "q", State: st, LastLine: c.line}
				if marked {
					s.RetireAt = bannerNow.Add(time.Hour)
				}
				lead, _ := bannerParts(m, s, false)
				require.Equal(t, c.want, lead, "%s, %s, marked %v", c.name, st, marked)
			}
		}
		s := protocol.SessionSummary{ID: "q", State: protocol.StateNeedsInput, LastLine: c.line, Title: "fix billing"}
		lead, _ := bannerParts(m, s, false)
		require.Equal(t, "task: fix billing", lead, "the task wins over the question: %s", c.name)
		s.Description = "porting billing"
		lead, _ = bannerParts(m, s, false)
		require.Equal(t, "porting billing", lead, "the summary wins: %s", c.name)
	}
}
