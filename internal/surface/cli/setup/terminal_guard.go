package setup

import (
	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// requireTerminal refuses a CLI change to a trust setting
// (settings.TrustSettingIDs) unless a person runs it at a terminal: text
// output with stdin and stdout terminals, as rex ws trust. Agent mode, -o
// and pipes exit 6 (needs_terminal), and no flag skips it, so an agent
// cannot widen skip_permissions or auto_trust_dirs through rex. It is a
// speed bump, not a boundary: config.yaml is a file the agent can edit.
// cmd names the command in the message; rerun is what to run yourself.
func requireTerminal(cmd, rerun string) error {
	p := core.Out()
	if p.Format != core.FormatText || !p.InTTY || !p.OutTTY {
		return core.Errorf(core.ExitOperationRefused, "", "%s: only at a terminal (not in agent mode, -o or a pipe); run %s yourself",
			cmd, protocol.FlattenDisplayText(rerun)).WithID("needs_terminal")
	}
	return nil
}

// requireTerminalForSetting is requireTerminal for a change to setting id;
// any setting outside settings.TrustSettingIDs stays scriptable.
func requireTerminalForSetting(cmd, id, rerun string) error {
	if !settings.IsTrustSetting(id) {
		return nil
	}
	return requireTerminal(cmd, rerun)
}
