package setup

import (
	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

// requireTerminal is core.RequireTerminal: a change to a trust setting
// (settings.TrustSettingIDs) works only when a person runs it at a
// terminal, as rex ws trust.
func requireTerminal(cmd, rerun string) error {
	return core.RequireTerminal(cmd, rerun)
}

// requireTerminalForSetting is requireTerminal for a change to setting id;
// any setting outside settings.TrustSettingIDs stays scriptable.
func requireTerminalForSetting(cmd, id, rerun string) error {
	if !settings.IsTrustSetting(id) {
		return nil
	}
	return requireTerminal(cmd, rerun)
}
