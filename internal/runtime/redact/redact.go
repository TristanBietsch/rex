// Package redact makes command lines safe to log. It keeps the shape of a
// command (names, flags, subcommands) and removes what a person typed into
// it: prompts, paths and other values.
package redact

import (
	"regexp"
	"strconv"
	"strings"
)

// maxKeptPositional is the longest positional argument kept verbatim.
// Subcommands and model tags ("run", "exec", "llama3") are short words;
// prompts and paths are not.
const maxKeptPositional = 16

// Policy tunes Argv for a command whose arguments have known roles. The zero
// Policy is the strict default: every flag value and every positional that is
// not a short word is replaced.
type Policy struct {
	// KeepValues names flags (without dashes) whose values are kept.
	KeepValues map[string]bool
	// FreeTextFrom is the 1-based index, among the positional arguments after
	// argv[0], from which every positional is free text and is replaced. The
	// first positional is the subcommand. Zero means none.
	FreeTextFrom int
	// Selectors is how many positionals right after the first are session
	// selectors: replaced by <sel:N bytes>, or kept when they are a short
	// hex id.
	Selectors int
}

var shortHexID = regexp.MustCompile(`^[0-9a-f]{4,8}$`)

// Argv returns a copy of argv that is safe to log. argv[0], flag names and
// "--" are kept. Replaced with "<redacted:N bytes>":
//   - the value of a flag, whether attached (--flag=value) or in the next
//     argument (--flag value; a boolean flag's following argument is
//     over-redacted, which is the safe direction), unless the policy keeps
//     that flag's value;
//   - every argument after "--", since those are operands such as prompts;
//   - a positional the policy marks as free text;
//   - any other positional that is not a short word.
func Argv(argv []string, p Policy) []string {
	out := make([]string, 0, len(argv))
	operands, flagValue, keepValue := false, false, false
	pos := 0 // positionals seen after argv[0]
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
			keep := p.KeepValues[strings.TrimLeft(name, "-")]
			switch {
			case !attached:
				out = append(out, arg)
			case keep:
				out = append(out, arg)
			default:
				out = append(out, name+"="+redacted(value))
			}
			flagValue, keepValue = !attached, keep
			continue
		case flagValue:
			if keepValue {
				out = append(out, arg)
			} else {
				out = append(out, redacted(arg))
			}
		default:
			pos++
			out = append(out, positional(arg, pos, p))
		}
		flagValue, keepValue = false, false
	}
	return out
}

// positional renders the pos-th positional argument under p.
func positional(arg string, pos int, p Policy) string {
	switch {
	case p.FreeTextFrom > 0 && pos >= p.FreeTextFrom:
		return redacted(arg)
	case pos >= 2 && pos < 2+p.Selectors:
		if shortHexID.MatchString(arg) {
			return arg
		}
		return "<sel:" + strconv.Itoa(len(arg)) + " bytes>"
	case isShortWord(arg):
		return arg
	}
	return redacted(arg)
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
