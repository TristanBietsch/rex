package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// The dry run names its target; a confirm that carries it is refused when a
// new turn moved the target meanwhile, and nothing changes.
func TestUndo_ExpectRefusesAChangedTarget(t *testing.T) {
	repo := undoRepo(t)
	e := startUndoServer(t, true, undoSession("a", repo, protocol.StateNeedsInput))
	agentTurn(t, e, repo, "a", "a.txt", "two\n")

	plan, code, msg := undoCall(t, e, protocol.IntentUndo, protocol.Undo{SessionID: "a", DryRun: true})
	require.Empty(t, code, msg)
	require.NotEmpty(t, plan.Target)

	agentTurn(t, e, repo, "a", "a.txt", "three\n") // the session ran another turn

	_, code, msg = undoCall(t, e, protocol.IntentUndo, protocol.Undo{SessionID: "a", Expect: plan.Target})
	require.Equal(t, protocol.ErrCodeBadState, code)
	require.Equal(t, "the session changed since the preview — run it again", msg)
	got, _ := os.ReadFile(filepath.Join(repo, "a.txt"))
	require.Equal(t, "three\n", string(got), "a refused undo changes nothing")

	// A fresh preview matches and goes through.
	plan, code, msg = undoCall(t, e, protocol.IntentUndo, protocol.Undo{SessionID: "a", DryRun: true})
	require.Empty(t, code, msg)
	_, code, msg = undoCall(t, e, protocol.IntentUndo, protocol.Undo{SessionID: "a", Expect: plan.Target})
	require.Empty(t, code, msg)
	got, _ = os.ReadFile(filepath.Join(repo, "a.txt"))
	require.Equal(t, "two\n", string(got))
}

func TestRedo_ExpectRefusesAChangedTarget(t *testing.T) {
	repo := undoRepo(t)
	e := startUndoServer(t, true, undoSession("a", repo, protocol.StateNeedsInput))
	agentTurn(t, e, repo, "a", "a.txt", "two\n")
	_, code, msg := undoCall(t, e, protocol.IntentUndo, protocol.Undo{SessionID: "a"})
	require.Empty(t, code, msg)

	plan, code, msg := undoCall(t, e, protocol.IntentRedo, protocol.Redo{SessionID: "a", DryRun: true})
	require.Empty(t, code, msg)
	require.NotEmpty(t, plan.Target)
	_, code, msg = undoCall(t, e, protocol.IntentRedo, protocol.Redo{SessionID: "a", Expect: "0000000000000000000000000000000000000000"})
	require.Equal(t, protocol.ErrCodeBadState, code)
	require.Contains(t, msg, "changed since the preview")
	_, code, msg = undoCall(t, e, protocol.IntentRedo, protocol.Redo{SessionID: "a", Expect: plan.Target})
	require.Empty(t, code, msg)
}
