package pty

import (
	"strconv"
	"strings"

	"github.com/tristanbietsch/rex/internal/daemon/state"
)

// maxKeptPositional is the longest positional argument redactArgv keeps
// verbatim. Subcommands and model tags ("run", "exec", "llama3") are short
// words; prompts and paths are not.
const maxKeptPositional = 16

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

// redactArgv returns a copy of argv that is safe to log. argv[0], flag names
// and "--" are kept. Replaced with "<redacted:N bytes>":
//   - the value of a flag, whether attached (--flag=value) or in the next
//     argument (--flag value; a boolean flag's following argument is
//     over-redacted, which is the safe direction);
//   - every argument after "--", since those are operands such as prompts;
//   - any other positional argument that is not a short word.
func redactArgv(argv []string) []string {
	out := make([]string, 0, len(argv))
	operands, flagValue := false, false
	for i, arg := range argv {
		switch {
		case i == 0:
			out = append(out, arg)
		case operands:
			out = append(out, redacted(arg))
		case arg == "--":
			out = append(out, arg)
			operands = true
		case isFlag(arg):
			name, value, attached := strings.Cut(arg, "=")
			if attached {
				out = append(out, name+"="+redacted(value))
			} else {
				out = append(out, arg)
			}
			flagValue = !attached
			continue
		case flagValue, !isShortWord(arg):
			out = append(out, redacted(arg))
		default:
			out = append(out, arg)
		}
		flagValue = false
	}
	return out
}

// isFlag reports whether arg looks like an option ("-m", "--model=x").
// A lone "-" is the conventional stdin operand, not a flag.
func isFlag(arg string) bool {
	return len(arg) > 1 && arg[0] == '-'
}

// isShortWord reports whether arg is a subcommand-like token: at most
// maxKeptPositional bytes of ASCII letters, digits and "._:-".
func isShortWord(arg string) bool {
	if arg == "" || len(arg) > maxKeptPositional {
		return false
	}
	for _, r := range arg {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '.', r == '_', r == ':', r == '-':
		default:
			return false
		}
	}
	return true
}

func redacted(s string) string {
	return "<redacted:" + strconv.Itoa(len(s)) + " bytes>"
}
