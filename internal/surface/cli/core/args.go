package core

import (
	"errors"
	"flag"
	"strings"
)

// ErrHelpShown is returned by ParseArgs after -h or -help printed the
// command's usage. It carries exit code 0 and an empty message, so main
// prints no error line.
var ErrHelpShown error = ExitError{Code: ExitOK}

// ParseArgs parses args into fs with flags allowed anywhere among the
// positional arguments, and returns the positionals in order. A bare "--"
// ends flag parsing: everything after it is positional, even when it starts
// with "-". A bare "-" is positional.
//
// -h and -help return ErrHelpShown. Any other parse error is an ExitError
// with ExitInvalidArgs. Callers use the returned slice, not fs.Args().
func ParseArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	return parseArgs(fs, args, -1)
}

// ParseTextArgs is ParseArgs for commands whose trailing positionals are
// free text (`new`, `reply`, `name`). The first leading positionals are
// ordinary arguments (a selector); after those, an unknown -x is an error
// that tells the user to put -- before text that starts with "-". Known
// flags are still parsed there.
func ParseTextArgs(fs *flag.FlagSet, args []string, leading int) ([]string, error) {
	if leading < 0 {
		panic("core.ParseTextArgs: negative leading")
	}
	return parseArgs(fs, args, leading)
}

func parseArgs(fs *flag.FlagSet, args []string, leading int) ([]string, error) {
	var tail []string
	for i, a := range args {
		if a == "--" {
			tail = append([]string(nil), args[i+1:]...)
			args = args[:i]
			break
		}
	}
	var pos []string
	for len(args) > 0 {
		if err := fs.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil, ErrHelpShown
			}
			msg := err.Error()
			if leading >= 0 && len(pos) >= leading && strings.HasPrefix(msg, "flag provided but not defined") {
				msg += "; use -- before text that starts with -"
			}
			return nil, NewExitError(ExitInvalidArgs, msg)
		}
		rest := fs.Args()
		if len(rest) == 0 {
			break
		}
		pos = append(pos, rest[0])
		args = rest[1:]
	}
	return append(pos, tail...), nil
}
