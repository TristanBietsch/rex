package tui

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func TestApplyPatch_DescriptionAt(t *testing.T) {
	var s protocol.SessionSummary
	applyPatch(&s, map[string]any{"description": "Running tests", "description_at": "2026-03-10T15:30:00Z"})
	require.Equal(t, "Running tests", s.Description)
	require.Equal(t, time.Date(2026, 3, 10, 15, 30, 0, 0, time.UTC), s.DescriptionAt.UTC())

	// A patch without the key (an older daemon) leaves it alone.
	applyPatch(&s, map[string]any{"description": "Editing x.go"})
	require.Equal(t, time.Date(2026, 3, 10, 15, 30, 0, 0, time.UTC), s.DescriptionAt.UTC())
}
