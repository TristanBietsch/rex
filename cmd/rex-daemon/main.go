// Package main is the rex-daemon entry point.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/daemon/boot"
	"github.com/tristanbietsch/rex/internal/daemon/server"
	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/daemon/termtext"
	"github.com/tristanbietsch/rex/internal/features/summarizer"
	"github.com/tristanbietsch/rex/internal/runtime/daemonctl"
	"github.com/tristanbietsch/rex/internal/runtime/rexlog"
)

const version = "v1"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "rex-daemon:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("rex-daemon", flag.ContinueOnError)
	socketPath := fs.String("socket", daemonctl.DefaultSocket(), "UDS path")
	stateDir := fs.String("state-dir", daemonctl.DefaultStateDir(), "state directory")
	toolsPath := fs.String("tools", daemonctl.DefaultToolsPath(), "path to tools.yaml override (optional)")
	printVersion := fs.Bool("version", false, "print version and exit")
	maxConcurrent := fs.Int("max-concurrent-sessions", 16, "cap on live PTY sessions")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if *printVersion {
		fmt.Println(version)
		return nil
	}

	rexlog.Init("daemon")
	defer rexlog.Close()
	slog.Info("daemon: starting", "version", version, "socket", *socketPath, "state_dir", *stateDir, "max_concurrent", *maxConcurrent)

	if err := daemonctl.CheckSocketPath(*socketPath); err != nil {
		slog.Error("daemon: socket path too long", "socket", *socketPath, "err", err)
		return err
	}

	reg, err := registry.Load(*toolsPath)
	if err != nil {
		slog.Error("daemon: registry load failed", "tools", *toolsPath, "err", err)
		return fmt.Errorf("registry: %w", err)
	}
	slog.Info("daemon: registry loaded", "tools", *toolsPath, "count", len(reg.Tools))

	if err := ownerOnlyDir(*stateDir, *stateDir == daemonctl.DefaultStateDir()); err != nil {
		return fmt.Errorf("state dir: %w", err)
	}
	if err := state.SecureSessionsDir(*stateDir); err != nil {
		return fmt.Errorf("sessions dir: %w", err)
	}
	// ~/.cache/rex is rex's own; $XDG_RUNTIME_DIR belongs to the system and
	// is already 0700.
	socketDir := filepath.Dir(*socketPath)
	ownSocketDir := os.Getenv("XDG_RUNTIME_DIR") == "" && socketDir == filepath.Dir(daemonctl.DefaultSocket())
	if err := ownerOnlyDir(socketDir, ownSocketDir); err != nil {
		return fmt.Errorf("socket dir: %w", err)
	}

	store := state.NewStore()
	prior, err := state.LoadAll(*stateDir)
	if err != nil {
		slog.Error("daemon: load prior sessions failed", "state_dir", *stateDir, "err", err)
		return fmt.Errorf("load prior sessions: %w", err)
	}
	slog.Info("daemon: prior sessions restored", "count", len(prior))
	for _, s := range prior {
		_ = store.Add(s)
	}
	// Older daemons allowed duplicate slugs. Keep the sessions (selectors
	// report them as ambiguous) but say so; renaming one resolves it.
	for slug, shorts := range store.DuplicateSlugs() {
		slog.Warn("daemon: duplicate slug in restored sessions (select by short id or rename)",
			"slug", slug, "short_ids", shorts)
	}

	// Load settings — same config.yaml the TUI writes to. Missing file is fine;
	// the registry defaults apply.
	settingsStore := settings.NewStore()
	if err := settingsStore.Load(settings.DefaultPath()); err != nil {
		slog.Warn("daemon: settings load failed (using defaults)", "path", settings.DefaultPath(), "err", err)
	}
	summaryEnabled, _ := settingsStore.Get("summary_enabled").(bool)
	summaryModel, _ := settingsStore.Get("summary_model").(string)
	if summaryModel == "" {
		summaryModel = summarizer.Defaults().Model
	}
	slog.Info("daemon: summarizer config", "enabled", summaryEnabled, "model", summaryModel)
	retireTTL := server.DefaultRetireTTL
	if h, ok := settingsStore.Get("retire_after_hours").(int); ok && h > 0 {
		retireTTL = time.Duration(h) * time.Hour
	}
	slog.Info("daemon: retire config", "default_ttl", retireTTL)

	var summaryCh chan<- string
	var summaryWorker *summarizer.Worker
	if summaryEnabled {
		cfg := summarizer.Defaults()
		cfg.Model = summaryModel
		if u := boot.OllamaBaseURL(); u != "" {
			cfg.BaseURL = u
		}
		summaryWorker = summarizer.New(cfg, store, func(id string, max int) []byte {
			// The supervisor's emulated screen is the readable view of TUIs;
			// fall back to the cleaned transcript for line-oriented tools.
			if scr := store.Screen(id); scr != "" {
				return []byte(scr)
			}
			b, _ := state.TranscriptTail(*stateDir, id, 4*max)
			return []byte(termtext.Tail(b, max))
		})
		// Direct: the worker's channel IS the channel the supervisor sends into.
		// The worker's buffer is 64; the supervisor sends non-blocking with a
		// `default:` skip, so a slow worker simply drops a tick (next tick retries).
		// A pump goroutine in between would only add buffering, not real back-pressure.
		summaryCh = summaryWorker.Channel()
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	var srv *server.Server
	srv, err = server.New(server.Config{
		Socket:                *socketPath,
		StateDir:              *stateDir,
		Registry:              reg,
		Store:                 store,
		MaxConcurrentSessions: *maxConcurrent,
		SummaryRequest:        summaryCh,
		RetireDefaultTTL:      retireTTL,
		OnShutdown:            cancel,
		OnReload:              func() error { return boot.ReloadRegistry(srv, *toolsPath) },
	})
	if err != nil {
		return fmt.Errorf("server: %w", err)
	}

	// Lua scripting hook. Best-effort: failure to init never blocks daemon startup.
	luaRT, luaCancel := boot.StartLuaRuntime(srv, store)
	if luaCancel != nil {
		defer luaCancel()
	}
	if luaRT != nil {
		defer luaRT.Close()
	}

	// Start summarizer worker + health probe (when enabled).
	if summaryWorker != nil {
		summaryWorker.SetHealthCallback(func(available bool, reason string) {
			slog.Info("daemon: summarizer health flip", "available", available, "reason", reason)
			srv.BroadcastSummarizerHealth(available, reason)
		})
		go func() {
			if err := summaryWorker.Start(ctx); err != nil && !errors.Is(err, context.Canceled) {
				slog.Warn("summarizer: worker exited", "err", err)
			}
		}()
		go boot.ProbeOllamaHealth(ctx, summaryWorker, summaryModel)
	}

	// SIGHUP → reload tools.yaml.
	go boot.ReloadOnHUP(ctx, srv, *toolsPath)

	fmt.Fprintf(os.Stderr, "rex-daemon %s listening on %s\n", version, *socketPath)
	slog.Info("daemon: listening", "socket", *socketPath)
	err = srv.Serve(ctx)
	if err != nil {
		slog.Error("daemon: serve exited with error", "err", err)
	} else {
		slog.Info("daemon: serve exited cleanly")
	}
	return err
}

// ownerOnlyDir creates dir (and missing parents) with mode 0700. An existing
// dir is tightened only when tighten is set — rex's default dirs, which older
// builds created 0755 — never a path the user passed that may be shared
// (-state-dir /tmp).
func ownerOnlyDir(dir string, tighten bool) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if tighten {
		return os.Chmod(dir, 0o700)
	}
	return nil
}
