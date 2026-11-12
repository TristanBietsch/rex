package core

import "github.com/tristanbietsch/rex/internal/catalog/settings"

// autoAgentSetting reads the cli_agent_auto feature: whether CLAUDECODE=1
// with a piped stdout switches agent mode on. A config that will not load
// reads the default (on).
func autoAgentSetting() bool {
	store, _, err := settings.LoadDefault()
	if err != nil {
		store = nil
	}
	return settings.FeatureOn(store, "cli_agent_auto")
}

// RequireText refuses an interactive command in any format but text: a
// board, an attached terminal or a wizard cannot be rendered as data. hint
// names the command that answers the same question.
func (p *Printer) RequireText(cmd, hint string) error {
	if p.Format == FormatText {
		return nil
	}
	return Errorf(ExitInvalidArgs, hint, "%s: interactive; it cannot run in %s mode (try `%s`)", cmd, p.Format, hint)
}
