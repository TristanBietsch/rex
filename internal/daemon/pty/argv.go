package pty

import (
	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/runtime/redact"
)

// argvLogAttrs describes the child's command line for slog without leaking
// the free text it carries: the initial prompt rides on argv (claude and
// codex take it after "--", gemini as --prompt-interactive=...), and daemon
// logs persist on disk and get attached to bug reports.
func argvLogAttrs(sess *state.Session, argv []string) []any {
	sum := sess.Summary()
	return []any{
		"tool", sum.ToolID,
		"model", sum.ModelID,
		"argc", len(argv),
		"argv", redactArgv(argv),
	}
}

// redactArgv returns a copy of argv that is safe to log (see redact.Argv with
// the zero policy): argv[0], flag names and "--" are kept; flag values,
// everything after "--" and any positional that is not a short word are
// replaced with "<redacted:N bytes>".
func redactArgv(argv []string) []string { return redact.Argv(argv, redact.Policy{}) }
