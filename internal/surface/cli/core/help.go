package core

import (
	"flag"
	"strings"
)

// outputFlagNames are the flags AddOutputFlags registers; per-command help
// lists them in their own Output section.
var outputFlagNames = map[string]bool{"o": true, "output": true, "agent": true, "no-color": true, "json": true}

// Help overview column widths: every Defs in `rex --help` uses them, so the
// names, args and descriptions line up across sections.
const (
	helpNameWidth = 10
	helpArgsWidth = 22
)

// helpView builds the top-level help: a title line, how to start, then one
// rule-line section per HelpSections entry with every command as name, short
// args and description in shared columns, and an Options section last.
func helpView() View {
	defs := func(items []Def) Defs {
		return Defs{Kind: "command", Items: items, NameWidth: helpNameWidth, ArgsWidth: helpArgsWidth}
	}
	v := View{
		Header{Title: "rex " + displayVersion(Version) + " · Runtime Executive for Agents"},
		Section{Title: "Usage", Rule: true},
		Defs{Items: []Def{
			{Name: "rex", Args: "[--quiet-boot]", Desc: "open the board (--quiet-boot: no sounds)"},
			{Name: "rex", Args: "<command> -h", Desc: "flags and examples for a command"},
		}, NameWidth: helpNameWidth, ArgsWidth: helpArgsWidth},
	}
	for _, sec := range HelpSections {
		var items []Def
		for _, c := range Commands {
			if c.Section == sec {
				items = append(items, Def{Name: c.Name, Args: c.Brief, Key: c.Name, Desc: c.Short,
					Extra: []Field{{"class", c.Class.String()}}})
			}
		}
		v = append(v, Section{Title: sec, Rule: true}, defs(items))
	}
	v = append(v,
		Section{Title: "Options", Rule: true},
		Defs{Items: []Def{
			{Name: "<sel>", Desc: "short id (4+ hex), slug or full id"},
			{Name: "-o", Args: "FORMAT", Desc: "text|txt|md|json|agent; txt when piped"},
			{Name: "--agent", Desc: "one line per record, no color, no prompts"},
			{Name: "--no-color", Desc: "never emit ANSI color"},
			{Name: "agents", Args: "rex agent-help", Desc: "the usage card for LLM agents"},
		}, NameWidth: helpNameWidth, ArgsWidth: helpArgsWidth},
	)
	return v
}

// displayVersion is Version for the help title: without the leading v, and
// "dev" for a development build (a Go pseudo-version or "dev").
func displayVersion(v string) string {
	v = strings.TrimPrefix(v, "v")
	if v == "" || v == "dev" || strings.HasPrefix(v, "0.0.0-") {
		return "dev"
	}
	return v
}

// helpData is the `-o json` payload of rex help.
type helpData struct {
	Version  string        `json:"version"`
	Commands []commandData `json:"commands"`
}

type commandData struct {
	Name    string   `json:"name"`
	Aliases []string `json:"aliases,omitempty"`
	Section string   `json:"section"`
	Args    string   `json:"args"`
	Desc    string   `json:"desc"`
	Class   string   `json:"class"`
}

func commandsData() []commandData {
	out := make([]commandData, len(Commands))
	for i, c := range Commands {
		out[i] = commandData{c.Name, c.Aliases, c.Section, c.Args, c.Short, c.Class.String()}
	}
	return out
}

// RunHelp prints the top-level help: `rex help`, `rex --help`. `rex help
// agent` prints the agent card. In agent mode the card is the help, so an
// agent never has to read the human layout. A command's own help is
// `rex <command> -h` (main routes `rex help <command>` there).
func RunHelp(args []string) error {
	fs := flag.NewFlagSet("help", flag.ContinueOnError)
	out := AddOutputFlags(fs, AllFormats)
	rest, err := out.Parse(fs, args)
	if err != nil {
		return err
	}
	if len(rest) > 1 {
		return NewExitError(ExitInvalidArgs, "help: at most one topic")
	}
	p := Out()
	if len(rest) == 1 {
		if rest[0] != "agent" {
			return Errorf(ExitInvalidArgs, "rex --help", "help: unknown topic %q (try `rex help agent`, or `rex <command> -h`)", rest[0])
		}
		return emitAgentCard(p)
	}
	if p.Format == FormatAgent {
		return emitAgentCard(p)
	}
	return p.Emit(Result{Cmd: "help", Data: helpData{Version: Version, Commands: commandsData()}, View: helpView()})
}

// flagData is one flag in a command's `-o json` help.
type flagData struct {
	Name    string `json:"name"`
	Arg     string `json:"arg,omitempty"`
	Default string `json:"default,omitempty"`
	Desc    string `json:"desc"`
}

// cmdHelpData is the `-o json` payload of `rex <command> -h`.
type cmdHelpData struct {
	Command string     `json:"command"`
	Usage   string     `json:"usage"`
	Desc    string     `json:"desc"`
	Flags   []flagData `json:"flags"`
	Formats []string   `json:"formats,omitempty"`
	Example string     `json:"example,omitempty"`
}

// showCommandHelp prints a command's help from its FlagSet and the command
// table, to stdout in the current format. ParseArgs calls it for -h.
func showCommandHelp(fs *flag.FlagSet) {
	o := outputFor(fs)
	p := Out()
	if o != nil {
		p, _ = o.build()
	}
	if p.Legacy { // -json -h: the versioned document is the only JSON help
		if np, err := p.WithFlags(FormatFlags{Output: "json"}); err == nil {
			p = np
		}
	}
	cmd, known := FindCommand(fs.Name())
	usage := strings.TrimSpace("rex " + fs.Name() + " " + cmd.Args)
	if !known {
		usage = "rex " + fs.Name() + " [flags]"
	}
	desc := cmd.Long
	if desc == "" {
		desc = cmd.Short
	}

	data := cmdHelpData{Command: fs.Name(), Usage: usage, Desc: desc, Flags: []flagData{}, Example: cmd.Example}
	var items, advanced []Def
	fs.VisitAll(func(f *flag.Flag) {
		if o != nil && outputFlagNames[f.Name] {
			return
		}
		arg, text := flag.UnquoteUsage(f)
		if bf, ok := f.Value.(interface{ IsBoolFlag() bool }); ok && bf.IsBoolFlag() {
			arg = ""
		}
		def := f.DefValue
		if def == "0" || def == "false" || def == "0s" {
			def = ""
		}
		fd := flagData{Name: "-" + f.Name, Arg: arg, Default: def, Desc: text}
		data.Flags = append(data.Flags, fd)
		if strings.HasPrefix(f.Usage, "(deprecated") {
			return // kept for old scripts and in -o json, not shown
		}
		if def != "" {
			text += " (default " + def + ")"
		}
		d := Def{Name: fd.Name, Args: argPlaceholder(f.Name, arg), Key: fd.Name, Desc: text,
			Extra: []Field{{"arg", arg}, {"default", def}}}
		if advancedFlags[f.Name] {
			advanced = append(advanced, d)
			return
		}
		items = append(items, d)
	})

	title := "rex " + fs.Name()
	if cmd.Short != "" {
		title += " · " + cmd.Short
	}
	v := View{
		Header{Title: title},
		Line{Text: "Usage: " + usage},
		Para{Text: desc},
	}
	// Flags, Advanced and Output share their columns.
	var output []Def
	if o != nil {
		output = outputDefs(o)
	}
	nameW, argsW := defsWidths(Defs{Items: append(append(append([]Def(nil), items...), advanced...), output...)})
	if len(items) > 0 {
		v = append(v, Section{Title: "Flags", Rule: true}, Defs{Kind: "flag", Items: items, NameWidth: nameW, ArgsWidth: argsW})
	}
	if len(advanced) > 0 {
		v = append(v, Section{Title: "Advanced", Rule: true}, Defs{Kind: "flag", Items: advanced, NameWidth: nameW, ArgsWidth: argsW})
	}
	if o != nil {
		data.Formats = formatNamesOf(o.supported)
		v = append(v, Section{Title: "Output", Rule: true}, Defs{Items: output, NameWidth: nameW, ArgsWidth: argsW})
	}
	if cmd.Example != "" {
		v = append(v, Section{Title: "Example", Rule: true}, Defs{Items: []Def{{Name: cmd.Example}}})
	}
	v = append(v, KV{Pairs: []Pair{
		{Key: "usage", Val: Cell{Text: usage}, Formats: SetOf(FormatAgent)},
		{Key: "desc", Val: Cell{Text: desc}, Formats: SetOf(FormatAgent)},
	}})
	_ = p.Emit(Result{Cmd: "help", Data: data, View: v})
}

// advancedFlags are shown under Advanced in a command's help: plumbing most
// users never set.
var advancedFlags = map[string]bool{"socket": true}

// argPlaceholder is a flag's argument as help shows it: upper case, and the
// flag's own name for a plain string ("-cwd CWD", "-wait DURATION").
func argPlaceholder(name, arg string) string {
	switch arg {
	case "":
		return ""
	case "string":
		return strings.ToUpper(name)
	}
	return strings.ToUpper(arg)
}

func outputDefs(o *OutputFlags) []Def {
	defs := []Def{
		{Name: "-o, --output", Args: "FORMAT", Desc: o.supported.String()},
		{Name: "--agent", Desc: "one line per record, no color, no prompts"},
		{Name: "--no-color", Desc: "never emit ANSI color"},
	}
	if o.legacy {
		defs = append(defs, Def{Name: "-json", Desc: "legacy JSON shape (prefer -o json)"})
	}
	return defs
}

func formatNamesOf(s FormatSet) []string {
	var names []string
	for i, n := range formatNames {
		if s.Has(Format(i)) {
			names = append(names, n)
		}
	}
	return names
}

// IsHelpArg reports whether a command group's first argument asks for
// help: -h, -help, --help or help.
func IsHelpArg(arg string) bool {
	switch arg {
	case "-h", "-help", "--help", "help":
		return true
	}
	return false
}

// RunGroupHelp prints the help of a command with subcommands (daemon,
// config, theme, ws, …) from the command table, honoring output flags in
// args, and returns ErrHelpShown. Groups call it when IsHelpArg(args[0]).
func RunGroupHelp(name string, args []string) error {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	out := AddOutputFlags(fs, AllFormats)
	if _, err := out.Parse(fs, append(append([]string(nil), args...), "-h")); err != nil {
		return err
	}
	return ErrHelpShown
}
