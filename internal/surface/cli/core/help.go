package core

import (
	"flag"
	"strings"
)

// outputFlagNames are the flags AddOutputFlags registers; per-command help
// lists them in their own Output section.
var outputFlagNames = map[string]bool{"o": true, "output": true, "agent": true, "no-color": true, "json": true}

// helpView builds the top-level help: Usage first, Title-case sections, no
// banners, ending with one hint line.
func helpView() View {
	v := View{
		Header{Title: "rex — Runtime Executive for Agents", Meta: Version},
		Section{Title: "Usage"},
		Defs{Items: []Def{
			{Name: "rex", Desc: "open the board (TUI)"},
			{Name: "rex --quiet-boot", Desc: "open the board without boot sounds"},
			{Name: "rex <command> [flags]"},
		}},
	}
	for _, sec := range HelpSections {
		var items []Def
		for _, c := range Commands {
			if c.Section == sec {
				items = append(items, Def{Name: strings.TrimSpace(c.Name + " " + c.Args), Key: c.Name, Desc: c.Short,
					Extra: []Field{{"class", c.Class.String()}}})
			}
		}
		v = append(v, Section{Title: sec}, Defs{Kind: "command", Items: items})
	}
	v = append(v,
		Section{Title: "Output"},
		Defs{Items: []Def{
			{Name: "-o, --output FORMAT", Desc: "text | txt | md | json | agent (txt when piped)"},
			{Name: "--agent", Desc: "one line per record, no color, no prompts"},
			{Name: "--no-color", Desc: "never emit ANSI color"},
		}},
		Para{Text: "Selectors: short id (4+ hex), slug or full id."},
		Para{Text: `Run "rex <command> -h" for flags · agents: rex agent-help`},
	)
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
	var items []Def
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
		name := fd.Name
		if arg != "" {
			name += " " + arg
		}
		if def != "" {
			text += " (default " + def + ")"
		}
		items = append(items, Def{Name: name, Key: fd.Name, Desc: text,
			Extra: []Field{{"arg", arg}, {"default", def}}})
	})

	v := View{Line{Text: "Usage: " + usage}, Para{Text: desc}}
	if len(items) > 0 {
		v = append(v, Section{Title: "Flags"}, Defs{Kind: "flag", Items: items})
	}
	if o != nil {
		data.Formats = formatNamesOf(o.supported)
		v = append(v, Section{Title: "Output"}, Defs{Items: outputDefs(o)})
	}
	if cmd.Example != "" {
		v = append(v, Section{Title: "Example"}, Defs{Items: []Def{{Name: cmd.Example}}})
	}
	v = append(v, KV{Pairs: []Pair{
		{Key: "usage", Val: Cell{Text: usage}, Formats: SetOf(FormatAgent)},
		{Key: "desc", Val: Cell{Text: desc}, Formats: SetOf(FormatAgent)},
	}})
	_ = p.Emit(Result{Cmd: "help", Data: data, View: v})
}

func outputDefs(o *OutputFlags) []Def {
	defs := []Def{
		{Name: "-o, --output FORMAT", Desc: o.supported.String()},
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
