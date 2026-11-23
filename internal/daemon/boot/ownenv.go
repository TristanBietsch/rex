//go:build !windows

package boot

import (
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"slices"
	"strings"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/daemon/childenv"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
)

// filteredMarker is set in the daemon's own environment once it has been
// filtered, so the daemon does not re-execute itself again. childenv.Build
// removes it from every child.
const filteredMarker = "REX_DAEMON_ENV_FILTERED"

// OwnEnv decides what the daemon's own environment should be. It is pure.
//
// needExec is false when parent already carries the filtered marker (a hot
// restart, or the second image after the re-exec) or when opts.Mode is
// inherit. Otherwise env is the allowlisted environment: the base names, the
// names in toolPass (the union of every tool's env_pass) and opts.Pass, plus
// the marker. Neither parent nor the slices are modified.
func OwnEnv(parent []string, opts childenv.Options, toolPass []string) (env []string, needExec bool) {
	if opts.Mode == childenv.ModeInherit || slices.Contains(parent, filteredMarker+"=1") {
		return nil, false
	}
	opts.Pass = slices.Concat(toolPass, opts.Pass)
	return append(childenv.Build(parent, opts), filteredMarker+"=1"), true
}

// ReexecFiltered re-executes the daemon in place (same pid) with its filtered
// environment, so the keys exported in the shell that started rex are no
// longer in the daemon's memory or its /proc/<pid>/environ. It does nothing
// when the environment is already filtered or child_env is inherit.
//
// It loads settings and the registry the way the daemon does at boot. When
// either fails it fails closed: it filters with the allowlist defaults (and
// no tool names for a registry that did not load) and logs a warning to
// stderr, instead of exec-ing with a guessed policy or stopping the daemon.
// exec is syscall.Exec in the daemon and a fake in tests; it is called at
// most once, with os.Executable() and os.Args. An error from exec is returned.
func ReexecFiltered(exec func(argv0 string, argv []string, env []string) error) error {
	parent := os.Environ()
	if slices.Contains(parent, filteredMarker+"=1") {
		return nil
	}
	opts := childenv.Options{Mode: childenv.ModeAllowlist, SSHAgent: true, GOOS: runtime.GOOS}
	var toolPass []string
	st, path, err := settings.LoadDefault()
	if err != nil {
		warnOwnEnv("settings load failed; filtering with the allowlist defaults", path, err)
	} else {
		opts = ChildEnv(st)
		if opts.Mode != childenv.ModeInherit {
			reg, err := LoadRegistry(toolsPathFromArgs(os.Args[1:]), st)
			if err != nil {
				warnOwnEnv("registry load failed; filtering without the tools' env_pass", "", err)
			} else {
				toolPass = registry.AllEnvPass(reg)
			}
		}
	}
	env, needExec := OwnEnv(parent, opts, toolPass)
	if !needExec {
		return nil
	}
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("filter environment: %w", err)
	}
	if err := exec(self, os.Args, env); err != nil {
		return fmt.Errorf("filter environment: re-exec: %w", err)
	}
	return nil
}

func warnOwnEnv(msg, path string, err error) {
	slog.Warn("daemon: "+msg, "path", path, "err", err)
	fmt.Fprintf(os.Stderr, "rex-daemon: %s: %v\n", msg, err)
}

// toolsPathFromArgs returns the value of the -tools / --tools flag in args,
// or the default tools.yaml path, so the registry loads from the file the
// daemon will use.
func toolsPathFromArgs(args []string) string {
	for i, a := range args {
		a = strings.TrimLeft(a, "-")
		if a == "tools" && i+1 < len(args) {
			return args[i+1]
		}
		if v, ok := strings.CutPrefix(a, "tools="); ok {
			return v
		}
	}
	if l, err := paths.Resolve(); err == nil {
		return l.ToolsFile
	}
	return ""
}
