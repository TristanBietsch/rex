package core

import (
	"flag"
	"fmt"
	"strings"
)

// ParseArgs parses args with fs and returns the positional arguments in
// order. Unlike fs.Parse, which stops at the first positional, flags may come
// before, between, or after positionals (`rex attach demo -read-only`). "--"
// ends flag parsing: everything after it is positional, so text starting with
// "-" is passed as `rex reply demo -- -1`. Errors are ExitInvalidArgs.
func ParseArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	var flags, pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			pos = append(pos, args[i+1:]...)
			break
		}
		if len(a) < 2 || a[0] != '-' {
			pos = append(pos, a)
			continue
		}
		flags = append(flags, a)
		// A non-boolean flag without "=value" takes the next argument as
		// its value, whatever it looks like (flag.Parse does the same).
		name := strings.TrimPrefix(strings.TrimPrefix(a, "-"), "-")
		if strings.Contains(name, "=") || i+1 == len(args) {
			continue
		}
		if f := fs.Lookup(name); f != nil && !isBoolFlag(f) {
			i++
			flags = append(flags, args[i])
		}
	}
	if err := fs.Parse(flags); err != nil {
		return nil, NewExitError(ExitInvalidArgs, err.Error())
	}
	return pos, nil
}

// isBoolFlag reports whether f is set by its name alone (-raw), mirroring the
// flag package's unexported boolFlag check.
func isBoolFlag(f *flag.Flag) bool {
	b, ok := f.Value.(interface{ IsBoolFlag() bool })
	return ok && b.IsBoolFlag()
}

// CheckArgs returns an ExitInvalidArgs error unless pos holds between min and
// max positionals (max < 0: no upper bound). usage is the command's synopsis,
// e.g. "rex attach <sel> [-read-only]".
func CheckArgs(pos []string, min, max int, usage string) error {
	if max >= 0 && len(pos) > max {
		return NewExitError(ExitInvalidArgs, fmt.Sprintf("unexpected argument %q (usage: %s)", pos[max], usage))
	}
	if len(pos) < min {
		return NewExitError(ExitInvalidArgs, "usage: "+usage)
	}
	return nil
}
