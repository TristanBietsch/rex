package boot

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/daemon/server"
	"github.com/tristanbietsch/rex/internal/daemon/state"
)

// catalogHome isolates HOME, writes config (if any) and puts a stub qwen on
// PATH that prints a synthetic model list and records each run in marker.
func catalogHome(t *testing.T, config string) (marker, cache string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("stub agents are POSIX shell scripts")
	}
	home := usageHome(t, config, false)
	bin := t.TempDir()
	marker = filepath.Join(t.TempDir(), "ran")
	script := "#!/bin/sh\n[ \"$1\" = --warm ] && exit 0\ntouch '" + marker + "'\nprintf 'MODEL\\nQwen3.8-Flash (qfmodel)\\n'\n"
	stub := filepath.Join(bin, "qwen")
	require.NoError(t, os.WriteFile(stub, []byte(script), 0o755)) //nolint:gosec // a test stub must be executable
	// The first run of a fresh script can take seconds on a loaded machine
	// (the OS scans it), longer than the built-in 5 s timeout: pay it now.
	require.NoError(t, exec.Command(stub, "--warm").Run())
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return marker, filepath.Join(home, ".local", "state", "rex", "models-cache.json")
}

// writeConfig replaces the isolated HOME's config.yaml.
func writeConfig(t *testing.T, content string) {
	t.Helper()
	cfg := filepath.Join(os.Getenv("HOME"), ".config", "rex", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(cfg), 0o700))
	require.NoError(t, os.WriteFile(cfg, []byte(content), 0o600))
}

func loadSettings(t *testing.T) *settings.Store {
	t.Helper()
	st, _, err := settings.LoadDefault()
	require.NoError(t, err)
	return st
}

func hasModel(reg *registry.Registry, tool, model string) bool {
	_, _, ok := reg.FindModel(tool, model)
	return ok
}

func newCatalogServer(t *testing.T, st *settings.Store) (*server.Server, *Catalog) {
	t.Helper()
	reg, err := LoadRegistry("", st)
	require.NoError(t, err)
	srv, err := server.New(server.Config{Socket: filepath.Join(t.TempDir(), "s.sock"), Registry: reg, Store: state.NewStore()})
	require.NoError(t, err)
	return srv, NewCatalog(srv, "")
}

func TestLoadRegistry_ExtraAgentsFlag(t *testing.T) {
	catalogHome(t, "")
	reg, err := LoadRegistry("", loadSettings(t))
	require.NoError(t, err)
	for _, id := range registry.ExtraAgentIDs {
		_, ok := reg.Find(id)
		require.True(t, ok, "on by default: %s", id)
	}

	writeConfig(t, "extra_agents: false\n")
	reg, err = LoadRegistry("", loadSettings(t))
	require.NoError(t, err)
	for _, id := range registry.ExtraAgentIDs {
		_, ok := reg.Find(id)
		require.False(t, ok, "off: %s is hidden", id)
		require.False(t, hasModel(reg, id, "default"), "off: %s is not spawnable", id)
	}
	_, ok := reg.Find("claude")
	require.True(t, ok, "other tools stay")
}

// Off is inert: no model command runs, the cache is neither read nor written.
func TestCatalog_ModelAutodetectOffIsInert(t *testing.T) {
	marker, cache := catalogHome(t, "")

	// Warm the cache with the flag on.
	srv, cat := newCatalogServer(t, loadSettings(t))
	cat.Refresh(context.Background(), loadSettings(t), false)
	cat.Wait()
	require.FileExists(t, marker)
	require.FileExists(t, cache)
	require.True(t, hasModel(srv.Registry(), "qwen", "qfmodel"), "detected model reaches the registry")

	// Now off, with the warm cache and a stub that would leave a marker.
	writeConfig(t, "model_autodetect: false\n")
	require.NoError(t, os.Remove(marker))
	before, err := os.Stat(cache)
	require.NoError(t, err)
	st := loadSettings(t)
	reg, err := LoadRegistry("", st)
	require.NoError(t, err)
	require.False(t, hasModel(reg, "qwen", "qfmodel"), "static models only")

	srv, cat = newCatalogServer(t, st)
	cat.Refresh(context.Background(), st, true)
	require.NoError(t, cat.Reload(context.Background(), st, true))
	cat.Wait()
	require.NoFileExists(t, marker, "no subprocess")
	after, err := os.Stat(cache)
	require.NoError(t, err)
	require.Equal(t, before.ModTime(), after.ModTime(), "cache untouched")
	require.False(t, hasModel(srv.Registry(), "qwen", "qfmodel"))
}

// A reload re-runs detection even when the cache is fresh; the cache serves
// the registry at once, before any command finishes.
func TestCatalog_ReloadRefreshes(t *testing.T) {
	marker, _ := catalogHome(t, "")
	st := loadSettings(t)
	srv, cat := newCatalogServer(t, st)
	require.NoError(t, cat.Reload(context.Background(), st, false))
	cat.Wait()
	require.FileExists(t, marker)
	require.True(t, hasModel(srv.Registry(), "qwen", "qfmodel"))

	require.NoError(t, os.Remove(marker))
	require.NoError(t, cat.Reload(context.Background(), st, false))
	require.True(t, hasModel(srv.Registry(), "qwen", "qfmodel"), "served from the cache at once")
	cat.Wait()
	require.NoFileExists(t, marker, "a fresh cache entry is not re-run without force")

	require.NoError(t, cat.ReloadDefault(context.Background()))
	cat.Wait()
	require.FileExists(t, marker, "rex reload forces a refresh")
}

func TestCatalog_ApplierReloadsOnFlagChange(t *testing.T) {
	catalogHome(t, "")
	st := loadSettings(t)
	srv, cat := newCatalogServer(t, st)
	apply := cat.Applier(context.Background(), st)

	_, ok := srv.Registry().Find("grok")
	require.True(t, ok)
	apply(st) // unchanged: nothing happens
	require.False(t, hasModel(srv.Registry(), "qwen", "qfmodel"))

	writeConfig(t, "extra_agents: false\n")
	off := loadSettings(t)
	apply(off)
	cat.Wait()
	_, ok = srv.Registry().Find("grok")
	require.False(t, ok, "extra_agents turned off at runtime")
}

// Refresh stops when its context ends and never swaps an older result over a
// newer reload.
func TestCatalog_StaleRefreshDropped(t *testing.T) {
	catalogHome(t, "")
	st := loadSettings(t)
	srv, cat := newCatalogServer(t, st)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cat.Refresh(ctx, st, true)
	cat.Wait()
	require.False(t, hasModel(srv.Registry(), "qwen", "qfmodel"), "canceled refresh swaps nothing")
}
