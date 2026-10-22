package session

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func TestUniqueSlug(t *testing.T) {
	sessions := []protocol.SessionSummary{{Slug: "fix-bug"}, {Slug: "fix-bug-2"}, {Slug: "session"}}
	require.Equal(t, "other", uniqueSlug("other", sessions))
	require.Equal(t, "fix-bug-3", uniqueSlug("fix-bug", sessions))
	require.Equal(t, "session-2", uniqueSlug("session", sessions))
	require.Equal(t, "x", uniqueSlug("x", nil))
}
