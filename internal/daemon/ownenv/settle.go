package ownenv

import (
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/daemon/childenv"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
)

// SettleChildEnv writes child_env into config.yaml the first time a daemon of
// this version starts, so an upgrade does not change what agents see.
//
// Before child_env existed, every agent inherited the daemon's whole
// environment. When child_env is not set (neither in config.yaml nor in
// config.lua) it decides once and pins the answer:
//
//   - lockPath (the daemon.lock in the data directory) exists: a daemon ran
//     here before, so this is an upgrade and the value is inherit, the old
//     behavior;
//   - otherwise this is a fresh install and the value is allowlist, written
//     too so the next start (which will find the lock) does not take it for
//     an upgrade.
//
// It returns the value it wrote, or "" when it wrote nothing: child_env was
// already set, or configPath is empty (no home directory). A config that does
// not load is an error and is left untouched; the caller then falls back to
// the allowlist default. Save changes only the child_env key, so the rest of
// the file and its permission bits are kept.
//
// It runs before the daemon filters its own environment (Unix) or before rex
// starts the daemon (Windows), so that filter already sees the pinned value.
// Not safe against a concurrent writer of config.yaml (see Store.Save).
func SettleChildEnv(configPath, lockPath string) (string, error) {
	if configPath == "" {
		return "", nil
	}
	st, _, err := settings.LoadLayered(configPath)
	if err != nil {
		return "", fmt.Errorf("settle child_env: %w", err)
	}
	if st.Source("child_env") != settings.SourceDefault {
		return "", nil
	}
	value := string(childenv.ModeAllowlist)
	if lockPath != "" {
		if _, err := os.Stat(lockPath); err == nil {
			value = string(childenv.ModeInherit)
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("settle child_env: %w", err)
		}
	}
	pin := settings.NewStore()
	if err := pin.Set("child_env", value); err != nil {
		return "", fmt.Errorf("settle child_env: %w", err)
	}
	if err := pin.Save(configPath); err != nil {
		return "", fmt.Errorf("settle child_env: %w", err)
	}
	return value, nil
}

// settle runs SettleChildEnv on the default config.yaml and daemon.lock. A
// failure is logged and ignored: child_env then stays at its allowlist
// default for this start, and the next start tries again.
func settle() {
	cfg, err := paths.ConfigFile()
	if err != nil {
		return // no home directory: LoadDefault reports it
	}
	lock, err := paths.DaemonLock()
	if err != nil {
		return
	}
	got, err := SettleChildEnv(cfg, lock)
	if err != nil {
		warn("could not record child_env in config.yaml", cfg, err)
		return
	}
	if got == string(childenv.ModeInherit) {
		slog.Info("daemon: upgrade keeps agents' full environment", "path", cfg, "child_env", got)
		fmt.Fprintf(os.Stderr, "rex-daemon: upgraded from an earlier rex: wrote child_env: inherit to %s, so agents keep the full environment as before; set child_env: allowlist to filter it\n", cfg)
	}
}
