package core

import (
	"fmt"
	"strings"
)

// Format is one way a command can render its result.
type Format uint8

// The output formats, in documentation order.
const (
	// FormatText is the human layout: styled, aligned, wrapped to the terminal.
	FormatText Format = iota
	// FormatTXT is plain and stable for grep and awk: no ANSI, no banners.
	FormatTXT
	// FormatMD is GitHub-flavored Markdown for pasting into issues.
	FormatMD
	// FormatJSON is one versioned JSON document.
	FormatJSON
	// FormatAgent is one key=value line per record plus an ok trailer.
	FormatAgent
)

var formatNames = [...]string{"text", "txt", "md", "json", "agent"}

// String returns the name used by -o and REX_OUTPUT.
func (f Format) String() string {
	if int(f) < len(formatNames) {
		return formatNames[f]
	}
	return fmt.Sprintf("format(%d)", f)
}

// ParseFormat parses an -o / REX_OUTPUT value. An unknown name is an
// ExitInvalidArgs error.
func ParseFormat(s string) (Format, error) {
	for i, n := range formatNames {
		if s == n {
			return Format(i), nil
		}
	}
	return 0, NewExitError(ExitInvalidArgs,
		fmt.Sprintf("unknown output format %q (use text, txt, md, json or agent)", s))
}

// FormatSet is a set of formats a command supports.
type FormatSet uint8

// SetOf returns the set holding fs.
func SetOf(fs ...Format) FormatSet {
	var s FormatSet
	for _, f := range fs {
		s |= 1 << f
	}
	return s
}

// Format sets used by command classes (see docs/cli.md).
var (
	// AllFormats suits query and action commands.
	AllFormats = SetOf(FormatText, FormatTXT, FormatMD, FormatJSON, FormatAgent)
	// TextFormats suits content and interactive commands.
	TextFormats = SetOf(FormatText, FormatTXT)
)

// Has reports whether f is in the set.
func (s FormatSet) Has(f Format) bool { return s&(1<<f) != 0 }

// String lists the members, for error messages.
func (s FormatSet) String() string {
	var names []string
	for i, n := range formatNames {
		if s.Has(Format(i)) {
			names = append(names, n)
		}
	}
	return strings.Join(names, ", ")
}

// FormatFlags is what the command line said about output, before the
// environment is consulted.
type FormatFlags struct {
	// Output is the last -o/--output value, or "agent" for --agent. Empty
	// means no flag was given.
	Output string
	// Legacy is -json: the format is JSON, in the command's pre-versioned shape.
	Legacy bool
	// NoColor is --no-color.
	NoColor bool
}

// Explicit reports whether any format flag (not --no-color) was given.
func (f FormatFlags) Explicit() bool { return f.Output != "" || f.Legacy }

// Resolution is the format ResolveFormat settled on.
type Resolution struct {
	Format Format
	// Legacy means -json: render through Result.Legacy, not the versioned
	// envelope.
	Legacy bool
	// Auto means agent mode was switched on by the environment, not by the
	// user typing a flag or REX_AGENT=1.
	Auto bool
}

// ResolveFormat picks the output format. Precedence, highest first:
//
//  1. Explicit flags. -json together with -o or --agent is a conflict
//     (ExitInvalidArgs).
//  2. REX_AGENT=1.
//  3. REX_OUTPUT.
//  4. Agent auto-detect: CLAUDECODE=1 with stdout not a terminal, unless
//     REX_AGENT=0.
//  5. text on a terminal, txt otherwise.
func ResolveFormat(fl FormatFlags, getenv func(string) string, stdoutTTY bool) (Resolution, error) {
	if fl.Legacy {
		if fl.Output != "" {
			return Resolution{}, NewExitError(ExitInvalidArgs,
				"conflicting output flags: -json cannot be combined with -o or --agent")
		}
		return Resolution{Format: FormatJSON, Legacy: true}, nil
	}
	if fl.Output != "" {
		f, err := ParseFormat(fl.Output)
		return Resolution{Format: f}, err
	}
	agent, err := agentEnv(getenv)
	if err != nil {
		return Resolution{}, err
	}
	if agent == agentOn {
		return Resolution{Format: FormatAgent}, nil
	}
	if v := getenv("REX_OUTPUT"); v != "" {
		f, err := ParseFormat(v)
		if err != nil {
			return Resolution{}, NewExitError(ExitInvalidArgs, "REX_OUTPUT: "+err.Error())
		}
		return Resolution{Format: f}, nil
	}
	if agent != agentOff && getenv("CLAUDECODE") == "1" && !stdoutTTY {
		return Resolution{Format: FormatAgent, Auto: true}, nil
	}
	if stdoutTTY {
		return Resolution{Format: FormatText}, nil
	}
	return Resolution{Format: FormatTXT}, nil
}

type agentSetting uint8

const (
	agentUnset agentSetting = iota
	agentOn
	agentOff
)

func agentEnv(getenv func(string) string) (agentSetting, error) {
	switch v := getenv("REX_AGENT"); v {
	case "":
		return agentUnset, nil
	case "1", "true":
		return agentOn, nil
	case "0", "false":
		return agentOff, nil
	default:
		return agentUnset, NewExitError(ExitInvalidArgs,
			fmt.Sprintf("REX_AGENT: invalid value %q (use 1 or 0)", v))
	}
}

// colorEnabled decides, for one stream, whether output may carry ANSI color.
//
// Only the text format is ever colored. --no-color and a non-empty NO_COLOR
// turn color off. CLICOLOR_FORCE (non-empty, not "0") turns it on even in a
// pipe, for `less -R`. Otherwise the stream must be a terminal and TERM must
// not be dumb.
func colorEnabled(f Format, streamTTY, noColorFlag bool, getenv func(string) string) bool {
	if f != FormatText || noColorFlag || getenv("NO_COLOR") != "" {
		return false
	}
	if force := getenv("CLICOLOR_FORCE"); force != "" && force != "0" {
		return true
	}
	return streamTTY && getenv("TERM") != "dumb"
}
