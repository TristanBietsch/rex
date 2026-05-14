// Package main is the rex-daemon entry point.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/tristanbietsch/rex/internal/registry"
	"github.com/tristanbietsch/rex/internal/server"
	"github.com/tristanbietsch/rex/internal/state"
)

const version = "0.0.1-plan-a"

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

	if err := fs.Parse(args); err != nil {
		return err
	}
	if *printVersion {
		fmt.Println(version)
		return nil
	}

	reg, err := registry.Load(*toolsPath)
	if err != nil {
		return fmt.Errorf("registry: %w", err)
	}

	if err := os.MkdirAll(*stateDir, 0o755); err != nil {
		return fmt.Errorf("state dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(*socketPath), 0o755); err != nil {
		return fmt.Errorf("socket dir: %w", err)
	}

	store := state.NewStore()
	prior, err := state.LoadAll(*stateDir)
	if err != nil {
		return fmt.Errorf("load prior sessions: %w", err)
	}
	for _, s := range prior {
		_ = store.Add(s)
	}

	srv, err := server.New(server.Config{
		Socket:   *socketPath,
		StateDir: *stateDir,
		Registry: reg,
		Store:    store,
	})
	if err != nil {
		return fmt.Errorf("server: %w", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	fmt.Fprintf(os.Stderr, "rex-daemon %s listening on %s\n", version, *socketPath)
	return srv.Serve(ctx)
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
