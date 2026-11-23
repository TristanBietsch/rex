package boot

import (
	"log/slog"
	"runtime"
	"strings"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/daemon/childenv"
)

// The functions below read one setting from st, normally SettingsHub.Current.
// The server calls them at each spawn, so a changed setting applies to the
// next session after a reload, without a daemon restart.

// SkipPermissions reads skip_permissions.
func SkipPermissions(st *settings.Store) bool {
	on, _ := st.Get("skip_permissions").(bool)
	return on
}

// ChildEnv reads child_env, env_ssh_agent and env_passthrough into the
// environment policy for this OS. Pass is the env_passthrough names (a fresh
// slice: trimmed, empty entries dropped); the spawn adds the tool's env_pass
// (see server.Config.ChildEnv).
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

// ResumeEnabled reads the resume feature switch.
func ResumeEnabled(st *settings.Store) bool { return settings.FeatureOn(st, "resume") }

// SpawnSkillsEnabled reads the spawn_skills feature switch.
func SpawnSkillsEnabled(st *settings.Store) bool { return settings.FeatureOn(st, "spawn_skills") }

// AutoTrustDirs reads auto_trust_dirs.
//
// A value settings.SplitTrustDirs refuses (a piece that is neither absolute
// nor ~/, as when a folder name holds the list separator) is ignored as a
// whole rather than guessed at: it trusts nothing.
func AutoTrustDirs(st *settings.Store) []string {
	dirs, err := settings.SplitTrustDirs(st.String("auto_trust_dirs"))
	if err != nil {
		slog.Warn("daemon: auto_trust_dirs ignored; trusting no folder", "err", err)
		return nil
	}
	return dirs
}
