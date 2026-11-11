package server

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/features/snapshot"
)

// The snapshot package's own wording reaches the user unchanged, with
// recovery advice kept; anything else is labelled as an undo failure.
func TestUndoError_KeepsUsefulMessages(t *testing.T) {
	blocked := &snapshot.BlockedError{Paths: []string{"vendor", "build"}}
	for name, err := range map[string]error{
		"not a repo": snapshot.ErrNotGitWorktree,
		"too old":    fmt.Errorf("%w (found 2.30)", snapshot.ErrGitTooOld),
		"ownership":  snapshot.ErrDubiousOwnership,
		"no git":     snapshot.ErrNoGit,
		"blocked":    fmt.Errorf("wrapped: %w", blocked),
		"half done":  errors.New("delete x: boom; the folder may be half restored: run rex redo to bring it back"),
	} {
		require.Equal(t, err.Error(), undoError(err).Error(), name)
	}
	require.Contains(t, undoError(blocked).Error(), "vendor, build")
	other := errors.New("disk full")
	require.Equal(t, "undo failed: disk full", undoError(other).Error())
	require.ErrorIs(t, undoError(other), other)
}
