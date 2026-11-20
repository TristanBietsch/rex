package core

import "github.com/tristanbietsch/rex/internal/wire/protocol"

// RequireTerminal refuses a command that grants trust (a trust setting,
// rex trust, rex new -trust) unless a person runs it at a terminal: text
// output with stdin and stdout terminals. Agent mode, -o and pipes exit 6
// (needs_terminal), and no flag skips it, so an agent cannot widen trust
// through rex. It is a speed bump, not a boundary: config.yaml is a file the
// agent can edit. cmd names the command in the message; rerun is what to run
// yourself.
func RequireTerminal(cmd, rerun string) error {
	p := Out()
	if p.Format != FormatText || !p.InTTY || !p.OutTTY {
		return Errorf(ExitOperationRefused, "", "%s: only at a terminal (not in agent mode, -o or a pipe); run %s yourself",
			cmd, protocol.FlattenDisplayText(rerun)).WithID("needs_terminal")
	}
	return nil
}
