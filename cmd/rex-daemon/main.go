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
	"strings"
	"syscall"
	"time"

	"github.com/tristanbietsch/rex/internal/lua"
	"github.com/tristanbietsch/rex/internal/protocol"
	"github.com/tristanbietsch/rex/internal/registry"
	"github.com/tristanbietsch/rex/internal/rexlog"
	"github.com/tristanbietsch/rex/internal/server"
	"github.com/tristanbietsch/rex/internal/settings"
	"github.com/tristanbietsch/rex/internal/state"
	"github.com/tristanbietsch/rex/internal/summarizer"
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
	socketPath := fs.String("socket", defaultSocketPath(), "UDS path")
	stateDir := fs.String("state-dir", defaultStateDir(), "state directory")
	toolsPath := fs.String("tools", defaultToolsPath(), "path to tools.yaml override (optional)")
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
	slog.Info("daemon: summarizer config", "enabled", summaryEnabled, "model", summaryModel)

	var summaryCh chan<- string
	var summaryWorker *summarizer.Worker
	if summaryEnabled {
		cfg := summarizer.Defaults()
		cfg.Model = summaryModel
		if env := os.Getenv("OLLAMA_HOST"); env != "" {
			if !strings.HasPrefix(env, "http") {
				env = "http://" + env
			}
			cfg.BaseURL = env
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
	luaRT, luaCancel := startLuaRuntime(srv, store)
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
		go probeOllamaHealth(ctx, summaryWorker, summaryModel)
	}

	// SIGHUP → reload tools.yaml.
	go reloadOnHUP(ctx, srv, *toolsPath)

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

// reloadOnHUP listens for SIGHUP and swaps in a freshly-loaded registry.
func reloadOnHUP(ctx context.Context, srv *server.Server, toolsPath string) {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGHUP)
	defer signal.Stop(sig)
	for {
		select {
		case <-ctx.Done():
			return
		case <-sig:
			reg, err := registry.Load(toolsPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "rex-daemon: reload failed: %v\n", err)
				slog.Error("daemon: SIGHUP reload failed", "tools", toolsPath, "err", err)
				continue
			}
			srv.SetRegistry(reg)
			fmt.Fprintln(os.Stderr, "rex-daemon: registry reloaded")
			slog.Info("daemon: SIGHUP reload ok", "tools", toolsPath, "count", len(reg.Tools))
		}
	}
}

// startLuaRuntime initializes the Lua scripting runtime and subscribes it to
// store events. Returns (nil, nil) if Lua is disabled or fails to init —
// daemon startup must not depend on user scripts.
func startLuaRuntime(srv *server.Server, store *state.Store) (*lua.Runtime, func()) {
	cfgPath := luaConfigPath()
	if cfgPath == "" {
		slog.Info("daemon: lua disabled (no config path)")
		return nil, nil
	}

	rt, err := lua.New(lua.Options{
		Sender: func(sessionID, text string) error {
			ch := srv.InputChannel(sessionID)
			if ch == nil {
				return fmt.Errorf("session %q has no input channel", sessionID)
			}
			payload := []byte(text)
			select {
			case ch <- payload:
				return nil
			case <-time.After(2 * time.Second):
				return errors.New("send timed out")
			}
		},
		Lister: func() []protocol.SessionSummary {
			return store.Snapshot()
		},
	})
	if err != nil {
		slog.Error("daemon: lua init failed", "err", err)
		return nil, nil
	}

	if err := rt.LoadFile(cfgPath); err != nil {
		slog.Error("daemon: lua load failed; runtime still active for future reloads", "path", cfgPath, "err", err)
	}

	cancel := store.Subscribe(func(e state.Event) {
		switch e.Kind {
		case state.EventAdded:
			if e.Summary != nil {
				_ = rt.OnEvent(protocol.EventSessionAdded, *e.Summary)
			}
		case state.EventUpdated:
			sess, ok := store.Get(e.SessionID)
			if !ok {
				return
			}
			_ = rt.OnEvent(protocol.EventSessionUpdated, sess.Summary())
		case state.EventRemoved:
			_ = rt.OnEvent(protocol.EventSessionRemoved, protocol.SessionRemoved{SessionID: e.SessionID})
		}
	})

	return rt, cancel
}

// luaConfigPath returns the resolved path to the user's init.lua, or "" if not configured.
func luaConfigPath() string {
	st := settings.NewStore()
	if err := st.Load(settings.DefaultPath()); err != nil {
		slog.Warn("daemon: settings load failed for lua path", "err", err)
	}
	raw := st.String("lua_config_path")
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "~/") {
		home, _ := os.UserHomeDir()
		raw = filepath.Join(home, raw[2:])
	}
	return raw
}

// probeOllamaHealth runs the initial Ollama reachability + model-presence check,
// then loops every 30s while the backend is marked unavailable. Each successful
// check that finds the configured model present marks the worker available;
// failures (unreachable / model missing) mark it unavailable with a reason.
func probeOllamaHealth(ctx context.Context, w *summarizer.Worker, model string) {
	cfg := summarizer.Defaults()
	cfg.Model = model
	if env := os.Getenv("OLLAMA_HOST"); env != "" {
		if !strings.HasPrefix(env, "http") {
			env = "http://" + env
		}
		cfg.BaseURL = env
	}
	client := summarizer.NewClient(cfg)
	check := func() {
		tCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		tags, err := client.Tags(tCtx)
		if err != nil {
			slog.Debug("daemon: ollama unreachable", "base_url", cfg.BaseURL, "err", err)
			w.MarkUnavailable("ollama unreachable")
			return
		}
		for _, t := range tags {
			if t == model {
				w.MarkAvailable()
				return
			}
		}
		slog.Debug("daemon: ollama reachable but model missing", "model", model, "tags", tags)
		w.MarkUnavailable("model not pulled: " + model)
	}
	check()
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if !w.BackendAvailable() {
				check()
			}
		}
	}
}

func defaultSocketPath() string {
	if r := os.Getenv("XDG_RUNTIME_DIR"); r != "" {
		return filepath.Join(r, "rex.sock")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "rex", "rex.sock")
}

func defaultStateDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "rex")
}

func defaultToolsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "rex", "tools.yaml")
}
