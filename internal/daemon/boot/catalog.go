package boot

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/daemon/server"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
)

// Feature switches read here (Settings → Features).
const (
	featureExtraAgents     = "extra_agents"
	featureModelAutodetect = "model_autodetect"
)

// loadBase loads the built-in registry merged with tools.yaml and applies
// extra_agents: switched off, qwen, cursor and grok are gone, so they are
// neither offered nor spawnable. It starts no subprocess and reads no cache.
func loadBase(toolsPath string, st *settings.Store) (*registry.Registry, error) {
	reg, err := registry.Load(toolsPath)
	if err != nil {
		return nil, err
	}
	if !settings.FeatureOn(st, featureExtraAgents) {
		reg = reg.Without(registry.ExtraAgentIDs...)
	}
	return reg, nil
}

// modelsCachePath is the models cache file; "" when the home directory is
// unknown (detection then runs uncached).
func modelsCachePath() string {
	p, err := paths.ModelsCacheFile()
	if err != nil {
		slog.Warn("daemon: models cache path", "err", err)
		return ""
	}
	return p
}

// LoadRegistry is the registry the daemon starts serving: tools.yaml merged
// over the built-ins, extra_agents applied, and, with model_autodetect on,
// the models detected earlier (read from the cache file; an old entry is
// fine here, Refresh brings it up to date). It runs no model command. With
// model_autodetect off the cache is not even read.
func LoadRegistry(toolsPath string, st *settings.Store) (*registry.Registry, error) {
	reg, err := loadBase(toolsPath, st)
	if err != nil {
		return nil, err
	}
	if !settings.FeatureOn(st, featureModelAutodetect) {
		return reg, nil
	}
	return reg.WithFeatures(true, true, modelsCachePath(), time.Now()), nil
}

// Catalog keeps the daemon's registry current: it reloads tools.yaml and
// settings, and refreshes each installed agent's model list in the
// background. One per daemon.
type Catalog struct {
	srv       *server.Server
	toolsPath string

	// gen counts reloads; a background refresh that started before the latest
	// reload drops its result instead of overwriting newer tools.yaml.
	gen atomic.Uint64
	wg  sync.WaitGroup

	// mu orders registry swaps: the generation check and SetRegistry in
	// Refresh and Reload happen under it, so an old refresh can never land
	// after a newer reload. cancel stops the running refresh.
	mu     sync.Mutex
	cancel context.CancelFunc
}

// NewCatalog returns a Catalog that swaps registries into srv.
func NewCatalog(srv *server.Server, toolsPath string) *Catalog {
	return &Catalog{srv: srv, toolsPath: toolsPath}
}

// Wait blocks until the background model refreshes have finished. Tests and
// shutdown use it; each refresh is bounded by its commands' timeouts.
func (c *Catalog) Wait() { c.wg.Wait() }

// Refresh detects models in the background and, when done, swaps the merged
// registry into the server. force ignores a cache entry that is still valid.
// With model_autodetect off it does nothing at all: no goroutine, no
// subprocess, no file read or write.
func (c *Catalog) Refresh(ctx context.Context, st *settings.Store, force bool) {
	if !settings.FeatureOn(st, featureModelAutodetect) {
		c.stopRefresh()
		return
	}
	// Each refresh has its own context: a reload (or the switch turning off)
	// cancels it, which kills its model commands and stops it writing the cache.
	rctx, cancel := context.WithCancel(ctx)
	c.mu.Lock()
	if c.cancel != nil {
		c.cancel()
	}
	c.cancel = cancel
	gen := c.gen.Load()
	c.mu.Unlock()
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		defer cancel()
		base, err := loadBase(c.toolsPath, st)
		if err != nil {
			slog.Warn("daemon: model refresh skipped", "err", err)
			return
		}
		detected := registry.RefreshModels(rctx, base.Tools, modelsCachePath(), force, time.Now())
		c.mu.Lock()
		defer c.mu.Unlock()
		if rctx.Err() != nil || c.gen.Load() != gen {
			return
		}
		c.srv.SetRegistry(base.WithModels(detected))
		slog.Info("daemon: models refreshed", "tools_with_detected_models", len(detected))
	}()
}

// stopRefresh cancels the running refresh, if any.
func (c *Catalog) stopRefresh() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}
}

// Reload re-reads tools.yaml and swaps the new registry in at once (static
// models plus whatever the cache holds), then refreshes the models in the
// background. Used by SIGHUP, the Reload intent and settings changes. On a
// load error the current registry stays.
func (c *Catalog) Reload(ctx context.Context, st *settings.Store, force bool) error {
	reg, err := LoadRegistry(c.toolsPath, st)
	if err != nil {
		fmt.Fprintf(os.Stderr, "rex-daemon: reload failed: %v\n", err)
		slog.Error("daemon: reload failed", "tools", c.toolsPath, "err", err)
		return fmt.Errorf("reload %s: %w", c.toolsPath, err)
	}
	LogRegistryWarnings(c.toolsPath, reg)
	c.mu.Lock()
	c.gen.Add(1)
	if c.cancel != nil {
		c.cancel() // the running refresh belongs to the old registry
	}
	c.srv.SetRegistry(reg)
	c.mu.Unlock()
	fmt.Fprintln(os.Stderr, "rex-daemon: registry reloaded")
	slog.Info("daemon: reload ok", "tools", c.toolsPath, "count", len(reg.Tools))
	c.Refresh(ctx, st, force)
	return nil
}

// ReloadDefault is Reload with the settings read now from config.yaml (a
// read error uses the defaults, like the other per-action reads), and force
// set: a reload is also how the user asks for fresh model lists.
func (c *Catalog) ReloadDefault(ctx context.Context) error {
	st, _, err := settings.LoadDefault()
	if err != nil {
		slog.Warn("daemon: settings load failed for reload; using what loaded", "err", err)
	}
	return c.Reload(ctx, st, true)
}

// Applier returns a WatchSettings applier that reloads the registry when
// extra_agents or model_autodetect changes. initial is the store the daemon
// started with.
func (c *Catalog) Applier(ctx context.Context, initial *settings.Store) func(*settings.Store) {
	extra := settings.FeatureOn(initial, featureExtraAgents)
	detect := settings.FeatureOn(initial, featureModelAutodetect)
	return func(st *settings.Store) {
		nextExtra := settings.FeatureOn(st, featureExtraAgents)
		nextDetect := settings.FeatureOn(st, featureModelAutodetect)
		if nextExtra == extra && nextDetect == detect {
			return
		}
		extra, detect = nextExtra, nextDetect
		slog.Info("daemon: catalog features changed", featureExtraAgents, extra, featureModelAutodetect, detect)
		if err := c.Reload(ctx, st, false); err != nil {
			slog.Warn("daemon: registry reload after a settings change failed", "err", err)
		}
	}
}

// ReloadOnHUP listens for SIGHUP and reloads the registry.
func ReloadOnHUP(ctx context.Context, c *Catalog) {
	reloadOnSignal(ctx, func() { _ = c.ReloadDefault(ctx) })
}
