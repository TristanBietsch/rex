// Package testenv isolates a test binary from the developer's real rex
// installation.
//
// rex resolves every path (config, state, socket, logs) from HOME and the
// XDG_* variables, and finds rex-daemon via PATH. A test that runs with the
// developer's environment can therefore read their config, dial their live
// daemon, or spawn their installed binaries. Every package's TestMain calls
// Run or RunLeakChecked so all tests start from an empty, throwaway home.
package testenv

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"go.uber.org/goleak"
)

// Run isolates the environment, runs the package's tests, and exits with
// their status. Call it from TestMain.
func Run(m *testing.M) {
	os.Exit(run(m, false))
}

// RunLeakChecked is Run plus a goroutine leak check after the tests pass.
// opts are forwarded to goleak (e.g. goleak.IgnoreTopFunction for a known,
// documented leak).
func RunLeakChecked(m *testing.M, opts ...goleak.Option) {
	os.Exit(run(m, true, opts...))
}

// run returns the exit code instead of exiting so the deferred temp-dir
// cleanup runs before os.Exit.
func run(m *testing.M, leakCheck bool, opts ...goleak.Option) int {
	dir, err := os.MkdirTemp(tempBase(), "rex-test-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "testenv: create temp home:", err)
		return 1
	}
	defer func() { _ = os.RemoveAll(dir) }()

	if err := Isolate(dir); err != nil {
		fmt.Fprintln(os.Stderr, "testenv:", err)
		return 1
	}

	code := m.Run()
	if code == 0 && leakCheck {
		if err := goleak.Find(opts...); err != nil {
			fmt.Fprintln(os.Stderr, "goleak: errors on successful test run:", err)
			code = 1
		}
	}
	return code
}

// Isolate points HOME, USERPROFILE, every XDG_* base dir and REX_LOG_DIR at
// fresh directories under dir. It also clears variables that would reach
// real services (OLLAMA_HOST) or change logging (REX_LOG_LEVEL), and drops
// the real ~/.local/bin from PATH so tests can't exec installed rex binaries.
func Isolate(dir string) error {
	realHome, _ := os.UserHomeDir()

	home := filepath.Join(dir, "home")
	dirs := map[string]string{
		"HOME":            home,
		"USERPROFILE":     home, // os.UserHomeDir on Windows
		"XDG_CONFIG_HOME": filepath.Join(home, ".config"),
		"XDG_DATA_HOME":   filepath.Join(home, ".local", "share"),
		"XDG_STATE_HOME":  filepath.Join(home, ".local", "state"),
		"XDG_CACHE_HOME":  filepath.Join(home, ".cache"),
		"XDG_RUNTIME_DIR": filepath.Join(dir, "run"),
		"REX_LOG_DIR":     filepath.Join(dir, "log"),
	}
	for key, path := range dirs {
		if err := os.MkdirAll(path, 0o700); err != nil {
			return fmt.Errorf("create %s dir: %w", key, err)
		}
		if err := os.Setenv(key, path); err != nil {
			return fmt.Errorf("set %s: %w", key, err)
		}
	}
	for _, key := range []string{"OLLAMA_HOST", "REX_LOG_LEVEL"} {
		if err := os.Unsetenv(key); err != nil {
			return fmt.Errorf("unset %s: %w", key, err)
		}
	}
	if realHome != "" {
		path := withoutDir(os.Getenv("PATH"), filepath.Join(realHome, ".local", "bin"))
		if err := os.Setenv("PATH", path); err != nil {
			return fmt.Errorf("set PATH: %w", err)
		}
	}
	return nil
}

// withoutDir removes every PATH entry equal to dir (ignoring a trailing
// separator).
func withoutDir(pathList, dir string) string {
	parts := filepath.SplitList(pathList)
	kept := parts[:0]
	for _, p := range parts {
		if filepath.Clean(p) != filepath.Clean(dir) {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, string(os.PathListSeparator))
}

// tempBase keeps temp paths short on Unix: macOS limits unix socket paths to
// 104 bytes, and the default $TMPDIR under /var/folders eats most of that.
func tempBase() string {
	if runtime.GOOS == "windows" {
		return ""
	}
	return "/tmp"
}
