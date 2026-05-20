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

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/daemon/boot"
	"github.com/tristanbietsch/rex/internal/daemon/server"
	"github.com/tristanbietsch/rex/internal/daemon/state"
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

	reg, err := registry.Load(*toolsPath)
	if err != nil {
		slog.Error("daemon: registry load failed", "tools", *toolsPath, "err", err)
		return fmt.Errorf("registry: %w", err)
	}
	slog.Info("daemon: registry loaded", "tools", *toolsPath, "count", len(reg.Tools))

	if err := os.MkdirAll(*stateDir, 0o755); err != nil {
		return fmt.Errorf("state dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(*socketPath), 0o755); err != nil {
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

	var summaryCh chan<- string
	var summaryWorker *summarizer.Worker
	if summaryEnabled {
		cfg := summarizer.Defaults()
		cfg.Model = summaryModel
		if u := boot.OllamaBaseURL(); u != "" {
			cfg.BaseURL = u
		}
		summaryWorker = summarizer.New(cfg, store, func(id string, max int) []byte {
			b, _ := state.TranscriptTail(*stateDir, id, max)
			return b
		})
		// Direct: the worker's channel IS the channel the supervisor sends into.
		// The worker's buffer is 64; the supervisor sends non-blocking with a
		// `default:` skip, so a slow worker simply drops a tick (next tick retries).
		// A pump goroutine in between would only add buffering, not real back-pressure.
		summaryCh = summaryWorker.Channel()
	}

	srv, err := server.New(server.Config{
		Socket:                *socketPath,
		StateDir:              *stateDir,
		Registry:              reg,
		Store:                 store,
		MaxConcurrentSessions: *maxConcurrent,
		SummaryRequest:        summaryCh,
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

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

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
