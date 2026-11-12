package boot

import (
	"log/slog"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
)

// The functions below read one setting from st, normally SettingsHub.Current.
// The server calls them at each spawn, so a changed setting applies to the
// next session after a reload, without a daemon restart.

// SkipPermissions reads skip_permissions.
func SkipPermissions(st *settings.Store) bool {
	on, _ := st.Get("skip_permissions").(bool)
	return on
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
