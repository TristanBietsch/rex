package boot

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/features/usage"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func usageSession(id string) *state.Session {
	return &state.Session{ID: id, ShortID: id[:4], ToolID: "claude", ModelID: "opus", Slug: "s-" + id, CWD: "/w",
		State: protocol.StateWorking, StartedAt: time.Now().UTC(), Tokens: 250}
}

func TestUsage_SessionEndAndPruneReachTheLedger(t *testing.T) {
	dir := t.TempDir()
	store := state.NewStore()
	u := NewUsage(context.Background(), dir, store, nil)
	t.Cleanup(u.Close)
	u.SetEnabled(true)

	require.NoError(t, store.Add(usageSession("aaaa1111")))
	require.NoError(t, store.Add(usageSession("bbbb2222")))
	require.NoError(t, store.Transition("aaaa1111", protocol.StateDone))
	require.NoError(t, store.Remove("bbbb2222")) // pruned while working

	u.Hub.Sync()
	got, err := usage.OpenLedger(filepath.Join(dir, paths.UsageLedgerFileName)).Latest()
	require.NoError(t, err)
	require.True(t, got["aaaa1111"].Ended)
	require.EqualValues(t, 250, got["aaaa1111"].Tokens)
	require.Contains(t, got, "bbbb2222", "a removed session keeps its ledger line")
}

// Off is inert: the usage hub writes no ledger and reads no file.
func TestUsage_OffIsInert(t *testing.T) {
	dir := t.TempDir()
	store := state.NewStore()
	u := NewUsage(context.Background(), dir, store, nil)
	u.SetEnabled(false)

	require.NoError(t, store.Add(usageSession("aaaa1111")))
	require.NoError(t, store.Transition("aaaa1111", protocol.StateDone))
	require.NoError(t, store.Remove("aaaa1111"))
	u.Close()

	require.NoFileExists(t, filepath.Join(dir, paths.UsageLedgerFileName))
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Empty(t, entries, "nothing was created in the state dir")
}

func TestUsage_ApplierFollowsTheSetting(t *testing.T) {
	store := state.NewStore()
	u := NewUsage(context.Background(), t.TempDir(), store, nil)
	t.Cleanup(u.Close)

	st := settings.NewStore()
	require.NoError(t, st.Set("usage", false))
	u.Applier()(st)
	require.False(t, u.Hub.Enabled())
	require.NoError(t, st.Set("usage", true))
	u.Applier()(st)
	require.True(t, u.Hub.Enabled())
}
