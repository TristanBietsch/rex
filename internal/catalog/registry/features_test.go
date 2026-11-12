package registry

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// WithFeatures is what a client process (the wizard, Settings) uses: it
// reads the daemon's cache, runs nothing, and honors both switches.
func TestWithFeatures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub agents are POSIX shell scripts")
	}
	reg, err := Load("")
	require.NoError(t, err)
	qwen, _ := reg.Find("qwen")
	dir := t.TempDir()
	marker := filepath.Join(t.TempDir(), "ran")
	stubBinary(t, dir, "qwen", "touch '"+marker+"'\nprintf 'MODEL\\nQwen3.8-Flash (qfmodel)\\n'")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	cache := filepath.Join(t.TempDir(), "models-cache.json")
	now := time.Now()
	RefreshModels(context.Background(), []Tool{qwen}, cache, false, now)
	require.FileExists(t, marker)
	require.NoError(t, os.Remove(marker))

	on := reg.WithFeatures(true, true, cache, now.Add(72*time.Hour)) // stale is shown
	_, m, ok := on.FindModel("qwen", "qfmodel")
	require.True(t, ok)
	require.Equal(t, []string{"--model", "qfmodel"}, m.Args)
	require.True(t, m.Detected)
	require.NoFileExists(t, marker, "a client never runs the model command")

	// The pickers' validation (rex setup, default_spawn_model) sees detected ids.
	got, changed, err := on.ResolveSpawn(SpawnChoice{Tool: "claude", Model: "opus"}, "qwen", "qfmodel", "high")
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, SpawnChoice{Tool: "qwen", Model: "qfmodel", Effort: "high"}, got)
	_, _, err = reg.ResolveSpawn(SpawnChoice{Tool: "claude", Model: "opus"}, "qwen", "qfmodel", "")
	require.ErrorContains(t, err, "unknown model")

	off := reg.WithFeatures(true, false, cache, now)
	_, _, ok = off.FindModel("qwen", "qfmodel")
	require.False(t, ok, "model_autodetect off: the cache is not read")

	noExtra := reg.WithFeatures(false, true, cache, now)
	for _, id := range ExtraAgentIDs {
		_, ok := noExtra.Find(id)
		require.False(t, ok, "extra_agents off hides %s", id)
	}
	_, _, err = noExtra.ResolveSpawn(SpawnChoice{Tool: "claude", Model: "opus"}, "qwen", "", "")
	require.ErrorContains(t, err, "unknown tool")
}
