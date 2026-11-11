package state

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func TestSkills_SurviveMetaRoundTrip(t *testing.T) {
	dir := t.TempDir()
	sess := &Session{ID: "id1", State: protocol.StateDone, Skills: []string{"review", "docs"}}
	require.Equal(t, []string{"review", "docs"}, sess.Summary().Skills)

	// Summary copies: a client cannot mutate the session through it.
	sum := sess.Summary()
	sum.Skills[0] = "changed"
	require.Equal(t, "review", sess.Skills[0])

	require.NoError(t, WriteMeta(dir, sess))
	got, err := LoadMeta(dir, "id1")
	require.NoError(t, err)
	require.Equal(t, []string{"review", "docs"}, got.Skills)

	none := &Session{ID: "id2", State: protocol.StateDone}
	require.Empty(t, fromSummary(none.Summary()).Skills)
}

// meta.json is not trusted: skill names are cleaned and capped on load.
func TestSanitizeSkills(t *testing.T) {
	many := make([]string, 40)
	for i := range many {
		many[i] = "s"
	}
	require.Len(t, sanitizeSkills(many), maxSkills)
	got := sanitizeSkills([]string{"ok", "\x1b[31mred", "", "\n", strings.Repeat("x", 500)})
	require.Equal(t, "ok", got[0])
	for _, n := range got {
		require.NotContains(t, n, "\x1b")
		require.LessOrEqual(t, len([]rune(n)), 64)
	}
	require.Nil(t, sanitizeSkills(nil))
}
