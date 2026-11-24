// Package ownenv decides the environment rex-daemon itself runs with: the
// allowlisted one, so it does not hold the keys exported in the shell that
// started it. It is platform-neutral: Unix re-executes the daemon with the
// result (boot.ReexecFiltered), Windows starts the daemon with it
// (daemonctl.Start).
package ownenv

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
// filtered, so it is not filtered again. childenv.Build removes it from every
// child.
const filteredMarker = "REX_DAEMON_ENV_FILTERED"

// ChildEnv is the child-environment policy the settings describe: Mode,
// SSHAgent, GOOS and Pass (the parsed env_passthrough).
func ChildEnv(st *settings.Store) childenv.Options {
	mode := childenv.ModeAllowlist
	if st.String("child_env") == string(childenv.ModeInherit) {
		mode = childenv.ModeInherit
	}
	ssh, _ := st.Get("env_ssh_agent").(bool)
	var pass []string
	for _, n := range strings.Split(st.String("env_passthrough"), ",") {
		if n = strings.TrimSpace(n); n != "" {
			pass = append(pass, n)
		}
	}
	return childenv.Options{Mode: mode, SSHAgent: ssh, GOOS: runtime.GOOS, Pass: pass}
}

// OwnEnv decides what the daemon's own environment should be. It is pure.
//
// needExec is false when parent already carries the filtered marker (a hot
// restart, or the second image after the re-exec) or when opts.Mode is
// inherit. Otherwise env is the allowlisted environment: the base names, the
// names in toolPass (the union of every tool's env_pass), opts.Pass and every
// REX_* variable (rex's own configuration), plus the marker. Neither parent nor the slices are modified.
func OwnEnv(parent []string, opts childenv.Options, toolPass []string) (env []string, needExec bool) {
	if opts.Mode == childenv.ModeInherit || slices.Contains(parent, filteredMarker+"=1") {
		return nil, false
	}
	// rex's own configuration (REX_LOG_LEVEL, REX_LOG_DIR, ...) stays in the
	// daemon. Children still do not get it: Build for a spawn has no REX_*
	// base name.
	opts.Pass = slices.Concat([]string{"REX_*"}, toolPass, opts.Pass)
	return append(childenv.Build(parent, opts), filteredMarker+"=1"), true
}

// FilteredEnv loads the settings and the registry the way the daemon does at
// boot (args are the daemon's arguments, for -tools) and returns OwnEnv for
// parent.
//
// It fails closed: when settings do not load it uses the allowlist defaults,
// and when the registry does not load it uses no tool names; either way it
// logs a warning to stderr and slog instead of guessing a policy.
func FilteredEnv(parent, args []string) (env []string, needExec bool) {
	if slices.Contains(parent, filteredMarker+"=1") || probeOnly(args) {
		return nil, false
	}
	settle()
	opts := childenv.Options{Mode: childenv.ModeAllowlist, SSHAgent: true, GOOS: runtime.GOOS}
	var toolPass []string
	st, path, err := settings.LoadDefault()
	if err != nil {
		warn("settings load failed; filtering with the allowlist defaults", path, err)
		return OwnEnv(parent, opts, nil)
	}
	opts = ChildEnv(st)
	if opts.Mode != childenv.ModeInherit {
		toolsPath := toolsPathFromArgs(args)
		reg, err := registry.Load(toolsPath)
		if err != nil {
			warn("registry load failed; filtering without the tools' env_pass", toolsPath, err)
		} else {
			// Every tool counts, extra_agents or not: that switch can be
			// turned on while the daemon runs, and the keys must be there then.
			toolPass = registry.AllEnvPass(reg)
		}
	}
	return OwnEnv(parent, opts, toolPass)
}

// probeOnly reports whether args ask for a one-shot answer (-version, -help,
// -handoff-check) that must not load config or replace the process.
func probeOnly(args []string) bool {
	for _, a := range args {
		a = strings.TrimLeft(a, "-")
		switch a {
		case "version", "h", "help", "handoff-check":
			return true
		}
		if strings.HasPrefix(a, "handoff-check=") {
			return true
		}
	}
	return false
}

func warn(msg, path string, err error) {
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
