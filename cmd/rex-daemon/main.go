// Package main is the rex-daemon entry point.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
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
	"github.com/tristanbietsch/rex/internal/features/overview"
	"github.com/tristanbietsch/rex/internal/features/summarizer"
	"github.com/tristanbietsch/rex/internal/features/usage"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/runtime/rexlog"
	"github.com/tristanbietsch/rex/internal/version"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == usage.TapSubcommand {
		os.Exit(runTap(os.Args[2:]))
	}
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "rex-daemon:", err)
		os.Exit(1)
	}
}

// runTap is `rex-daemon statusline-tap --session <id> [--socket path]`, the
// status-line command rex gives Claude Code. Its stdout is Claude's status
// line, so it never prints a message of its own; a flag it cannot parse only
// means no usage is sent. It does not start logging or touch any state.
func runTap(args []string) int {
	fs := flag.NewFlagSet(usage.TapSubcommand, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	session := fs.String("session", "", "rex session id")
	socket := fs.String("socket", "", "daemon socket (default: the standard one)")
	if err := fs.Parse(args); err != nil {
		*session = "" // run the user's command, send nothing
	}
	return usage.RunTap(context.Background(), usage.TapOptions{
		Session: *session, Socket: *socket,
		Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr,
	})
}

func run(args []string) error {
	fs := flag.NewFlagSet("rex-daemon", flag.ContinueOnError)
	// Path defaults come from package paths. Without a home directory they
	// are empty, and requireDefaults fails unless the flag was given.
	layout, homeErr := paths.Resolve()
	defaultSocket, socketErr := paths.Socket()
	socketPath := fs.String("socket", defaultSocket, "UDS path")
	stateDir := fs.String("state-dir", layout.DataDir, "state directory")
	toolsPath := fs.String("tools", layout.ToolsFile, "path to tools.yaml override (optional)")
	skillsDir := fs.String("skills-dir", layout.SkillsDir, "skill library directory (rex new -skill)")
	printVersion := fs.Bool("version", false, "print version and exit")
	maxConcurrent := fs.Int("max-concurrent-sessions", 16, "cap on live PTY sessions")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if *printVersion {
		fmt.Println(version.Version)
		return nil
	}
	if err := requireDefaults(fs, map[string]error{
		"socket": socketErr, "state-dir": homeErr, "tools": homeErr,
	}); err != nil {
		return err
	}

	rexlog.Init("daemon")
	defer rexlog.Close()
	slog.Info("daemon: starting", "version", version.Version, "socket", *socketPath, "state_dir", *stateDir, "max_concurrent", *maxConcurrent)

	reg, err := registry.Load(*toolsPath)
	if err != nil {
		slog.Error("daemon: registry load failed", "tools", *toolsPath, "err", err)
		return fmt.Errorf("registry: %w", err)
	}
	boot.LogRegistryWarnings(*toolsPath, reg)
	slog.Info("daemon: registry loaded", "tools", *toolsPath, "count", len(reg.Tools))

	// Owner-only: sessions hold prompts, cwds and full agent output.
	if err := os.MkdirAll(*stateDir, 0o700); err != nil {
		return fmt.Errorf("state dir: %w", err)
	}
	// One daemon per state dir, held for the process lifetime: taken before
	// any state is read, released last (defers run in reverse).
	lock, err := state.AcquireLock(*stateDir)
	if err != nil {
		return lockError(err, *socketPath)
	}
	defer func() {
		if err := lock.Release(); err != nil {
			slog.Warn("daemon: release state lock", "err", err)
		}
	}()
	// Older daemons wrote 0755 dirs and 0644 files; fix them before loading.
	if err := state.TightenPerms(*stateDir); err != nil {
		slog.Warn("daemon: could not make state dir owner-only", "state_dir", *stateDir, "err", err)
	}
	// Only a dir we create is made 0700: an existing socket dir (such as
	// $XDG_RUNTIME_DIR) is not ours to chmod. The socket itself is 0600.
	if err := os.MkdirAll(filepath.Dir(*socketPath), 0o700); err != nil {
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
	settingsStore, settingsPath, err := settings.LoadDefault()
	if err != nil {
		slog.Warn("daemon: settings load failed (using defaults)", "path", settingsPath, "err", err)
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
		cfg.Detail = summarizer.ParseDetail(settingsStore.String("summary_detail"))
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

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	// After the first signal (or a Shutdown intent), restore the default
	// handlers: a second SIGINT/SIGTERM then kills a daemon whose graceful
	// shutdown is stuck instead of being swallowed.
	context.AfterFunc(ctx, stop)

	var srv *server.Server
	// Usage tracking: inert while the usage setting is off. Its callbacks
	// read srv only once Serve has started, after that assignment.
	usageHub := boot.NewUsage(ctx, *stateDir, store, func(u protocol.Usage) { srv.SetUsage(u) })
	defer usageHub.Close()
	// One settings watcher: every feature adds an applier here.
	appliers := []func(*settings.Store){usageHub.Applier()}

	// ov is set below when the summarizer runs; the callbacks read it only
	// once Serve has started, after that assignment.
	var ov *boot.Overview
	srv, err = server.New(server.Config{
		Socket:                *socketPath,
		StateDir:              *stateDir,
		Registry:              reg,
		Store:                 store,
		MaxConcurrentSessions: *maxConcurrent,
		SummaryRequest:        summaryCh,
		RetireDefaultTTL:      retireTTL,
		AutoTrustDirs:         boot.AutoTrustDirs,
		SkipPermissions:       boot.SkipPermissions,
		ResumeEnabled:         boot.ResumeEnabled,
		SkillsDir:             *skillsDir,
		SkillsEnabled:         boot.SpawnSkillsEnabled,
		UsageTap:              boot.UsageTap(*socketPath),
		OnUsageSample:         usageHub.Hub.Sample,
		UsageQuery:            usageHub.Hub.Query,
		OnShutdown:            stop,
		OnBoardClient:         func() { ov.Notify(overview.StructuralChange) },
		OnReload:              func() error { return boot.ReloadRegistry(srv, *toolsPath) },
	})
	if err != nil {
		return fmt.Errorf("server: %w", err)
	}

	// Started only now: switching usage on publishes its state through srv.
	usageHub.SetEnabled(settings.FeatureOn(settingsStore, "usage"))

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
		// The overview shares the summarizer's model; with overview_enabled
		// off it holds no goroutine and makes no call.
		ov = boot.NewOverview(ctx, overview.DefaultConfig(), store, srv, summaryWorker)
		defer ov.Close()
		ov.SetEnabled(boot.OverviewEnabled(settingsStore))
		summaryWorker.SetHealthCallback(func(available bool, reason string) {
			slog.Info("daemon: summarizer health flip", "available", available, "reason", reason)
			srv.BroadcastSummarizerHealth(available, reason)
			if available {
				ov.Notify(overview.StructuralChange) // retry what waited for the backend
			}
		})
		go func() {
			if err := summaryWorker.Start(ctx); err != nil && !errors.Is(err, context.Canceled) {
				slog.Warn("summarizer: worker exited", "err", err)
			}
		}()
		go boot.ProbeOllamaHealth(ctx, summaryWorker, summaryModel)
		appliers = append(appliers, boot.SummaryDetailApplier(summaryWorker), ov.Applier())
	}
	go boot.WatchSettings(ctx, settingsPath, 5*time.Second, appliers...)

	// SIGHUP → reload tools.yaml.
	go boot.ReloadOnHUP(ctx, srv, *toolsPath)

	fmt.Fprintf(os.Stderr, "rex-daemon %s listening on %s\n", version.Version, *socketPath)
	slog.Info("daemon: listening", "socket", *socketPath)
	err = srv.Serve(ctx)
	if err != nil {
		slog.Error("daemon: serve exited with error", "err", err)
	} else {
		slog.Info("daemon: serve exited cleanly")
	}
	return err
}

// lockError explains a failed state-dir lock. When another daemon holds it,
// the message names that daemon's pid and the socket this one would have
// served, so the user knows which process to stop.
func lockError(err error, socket string) error {
	var held *state.LockedError
	if !errors.As(err, &held) {
		return fmt.Errorf("state lock: %w", err)
	}
	pid := "unknown pid"
	if held.PID > 0 {
		pid = fmt.Sprintf("pid %d", held.PID)
	}
	slog.Error("daemon: another daemon holds the state dir", "lock", held.Path, "holder", pid, "socket", socket)
	return fmt.Errorf("another rex-daemon is already running (%s, lock %s); not starting a second one on socket %s: %w",
		pid, held.Path, socket, err)
}

// requireDefaults fails when a path flag was left at its default and that
// default could not be resolved (resolveErrs maps flag names to the
// resolution error, nil when it resolved). A flag the user set, even to "",
// is taken as given.
func requireDefaults(fs *flag.FlagSet, resolveErrs map[string]error) error {
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	for _, name := range []string{"socket", "state-dir", "tools"} {
		if err := resolveErrs[name]; err != nil && !set[name] {
			return fmt.Errorf("no default for -%s (pass it explicitly): %w", name, err)
		}
	}
	return nil
}
