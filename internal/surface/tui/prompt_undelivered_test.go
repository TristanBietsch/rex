package tui

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// A session whose first prompt was never typed in says so in the desc
// column, the simple view's text column and the summary preview, ahead of
// its task, summary or question.
func TestPromptUndelivered_ShownInPlaceOfTheTask(t *testing.T) {
	s := protocol.SessionSummary{ID: "a", Title: "fix auth", Description: "editing auth.go", LastLine: "proceed?"}
	require.Equal(t, "editing auth.go", rowDescription(s))

	applyPatch(&s, map[string]any{"prompt_undelivered": true})
	require.True(t, s.PromptUndelivered)
	require.Equal(t, promptUndeliveredText, rowDescription(s))
	require.Equal(t, promptUndeliveredText, rowText(s))
	lead, isSummary := previewLeadText(s)
	require.Equal(t, promptUndeliveredText, lead)
	require.False(t, isSummary)
}
