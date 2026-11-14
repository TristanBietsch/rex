package tui

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// undo_complete off is inert: the uc chord and :uc refuse, nothing is sent,
// and the :undone fix (unmark only) keeps working.
func TestUndoCompleteOff_IsInert(t *testing.T) {
	off := func() (Model, *fakeRetirer) {
		m, fr := ucModel()
		m.Store = flagOff(t, "undo_complete")
		return m, fr
	}
	t.Run("on by default", func(t *testing.T) {
		m, _ := ucModel()
		require.True(t, m.featureOn("undo_complete"))
	})
	t.Run("chord unbound", func(t *testing.T) {
		m, fr := off()
		m = typeKeys(m, "u")
		require.Empty(t, m.PendingChord)
		m, cmd := updateKey(m, key("c"))
		runCmd(cmd)
		require.Equal(t, "c", m.PendingChord, "c arms cc, not uc")
		require.Empty(t, fr.unretired)
		require.Empty(t, fr.retired)
	})
	t.Run("command refuses", func(t *testing.T) {
		m, fr := off()
		m, cmd := executeCommand(m, "uc")
		require.Equal(t, "off: enable in Settings → Features (rex config set undo_complete true)", m.Err)
		require.Nil(t, cmd)
		require.Empty(t, fr.unretired)
	})
	t.Run("undone still unmarks", func(t *testing.T) {
		m, fr := off()
		_, cmd := executeCommand(m, "undone")
		runCmd(cmd)
		require.Equal(t, []string{"a"}, fr.unretired)
	})
}
