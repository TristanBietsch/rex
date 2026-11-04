package core

import "log/slog"

// init silences log/slog (and the log package, which slog.SetDefault
// reroutes) for the rex CLI.
//
// Every CLI command imports core, and nothing else does: the daemon and the
// TUI packages don't. Without this, slog's default handler prints INFO
// lines to stderr. That clutters scripted output, and during `rex attach`
// it paints lines like "attach: redraw kick sent" into the raw alt-screen.
// Commands report failures by returning errors, which main prints, so
// stderr stays for real errors.
//
// Commands that want a log file call rexlog.Init (the board TUI and
// `rex setup` do). It calls slog.SetDefault itself and so replaces this
// handler; that is why this does not call rexlog.Init, whose sync.Once
// would turn those later calls into no-ops.
func init() {
	slog.SetDefault(slog.New(slog.DiscardHandler))
}
