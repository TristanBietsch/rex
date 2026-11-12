package tui

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// asking is a described session waiting on a question on its screen.
func asking(id, desc, question string) protocol.SessionSummary {
	s := described(id, desc)
	s.State = protocol.StateNeedsInput
	s.LastLine = question
	return s
}

func TestPreview_IconsLeadOrder(t *testing.T) {
	q := asking("a", "Editing x.go", "  Run the migration now?  ")
	text, isSummary := previewLeadText(q)
	require.Equal(t, "Run the migration now?", text, "the question beats the summary")
	require.False(t, isSummary)

	done := q
	done.RetireAt = previewNow
	text, isSummary = previewLeadText(done)
	require.Equal(t, "Editing x.go", text, "a row marked done asks nothing")
	require.True(t, isSummary)

	notQ := asking("a", "Editing x.go", "Checking for updates")
	text, _ = previewLeadText(notQ)
	require.Equal(t, "Editing x.go", text, "a last line that is not a question is not the lead")

	task := described("a", "")
	text, isSummary = previewLeadText(task)
	require.Equal(t, "task: fix invoice rounding", text, "no summary: the task")
	require.False(t, isSummary)

	text, _ = previewLeadText(protocol.SessionSummary{ID: "a"})
	require.Empty(t, text)

	// Rendered: the age suffix only follows the summary.
	m := previewModel(30, q)
	got := previewLine(plain(m, 30))
	require.Contains(t, got, "↳ Run the migration now?")
	require.NotContains(t, got, "· 12s")

	m.Sessions = []protocol.SessionSummary{described("a", "Editing x.go")}
	require.Contains(t, previewLine(plain(m, 30)), "↳ Editing x.go · 12s")

	m.Sessions = []protocol.SessionSummary{task}
	got = previewLine(plain(m, 30))
	require.Contains(t, got, "↳ task: fix invoice rounding")
	require.NotContains(t, got, "·")
}

func TestPreview_IconsBothViews(t *testing.T) {
	m := previewModel(30, described("a", "Editing x.go"))
	require.True(t, rowIconsOn(m))
	require.Contains(t, previewLine(plain(m, 30)), "↳ Editing x.go · 12s", "simple view")

	m.Detailed = true
	require.Contains(t, previewLine(plain(m, 30)), "↳ Editing x.go · 12s", "detailed view: no row text column, so the line shows")
}

func TestPreview_IconsReservedForTitleOnly(t *testing.T) {
	titled := protocol.SessionSummary{ID: "a", Slug: "a", Title: "fix invoice rounding", State: protocol.StateWorking}
	bare := protocol.SessionSummary{ID: "b", Slug: "b", State: protocol.StateWorking}
	m := previewModel(40, titled, bare)
	require.Equal(t, 1, previewRows(m), "a task is a lead: the row is reserved")
	require.Contains(t, previewLine(plain(m, 40)), "↳ task: fix invoice rounding")

	m.SelectedID = "b"
	require.Equal(t, 1, previewRows(m), "the reservation does not follow the selection")
	require.Empty(t, previewLine(plain(m, 40)), "no lead: the line is blank")

	m.Sessions = []protocol.SessionSummary{bare}
	require.Equal(t, 0, previewRows(m), "no session has a lead")

	m.Sessions = []protocol.SessionSummary{titled}
	m.Width = 50 // mobile layout
	require.Equal(t, 0, previewRows(m))
}

func TestPreview_OffUnchanged(t *testing.T) {
	// Off: a title alone reserves nothing, and the detailed view still hides
	// a description the row shows in full.
	titled := protocol.SessionSummary{ID: "a", Slug: "a", Title: "fix invoice rounding", State: protocol.StateWorking}
	m := rowIconsOff(previewModel(40, titled))
	require.Equal(t, 0, previewRows(m))

	m = rowIconsOff(previewModel(30, asking("a", "Editing x.go", "Run the migration now?")))
	require.Contains(t, previewLine(plain(m, 30)), "↳ Editing x.go · 12s", "off: the description, never the question")
	m.Detailed = true
	require.Empty(t, previewLine(plain(m, 30)), "off: the row already shows all of it")
}
