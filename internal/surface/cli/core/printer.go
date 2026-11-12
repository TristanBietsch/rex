package core

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Env is everything the Printer needs to know about the process: variables,
// terminals and platform. Tests build it by hand.
type Env struct {
	Getenv func(string) string
	// OutTTY, ErrTTY and InTTY say whether stdout, stderr and stdin are terminals.
	OutTTY, ErrTTY, InTTY bool
	GOOS                  string
	// Width is the render width for the text format. Zero means 80.
	Width int
	// Scheme and Spinner override the theme and spinner settings. When
	// both are empty and the format is text, the user's config is read
	// (with accent_color). Accent overrides accent_color with Scheme.
	Scheme, Spinner, Accent string
	// TrueColor pins the TrueColor profile when color is on, instead of
	// detecting one.
	TrueColor bool
}

// Printer renders command results. One is set up per process by InitOutput;
// each stream has its own color decision and renderer, so ANSI never reaches
// a redirected stream.
type Printer struct {
	Format Format
	// Legacy is -json: commands render their pre-versioned JSON shape.
	Legacy bool
	// AgentAuto is true when the environment, not the user, chose agent mode.
	AgentAuto bool

	Out, Err io.Writer
	// In is where Confirm reads the answer (stdin).
	In io.Reader
	// OutTTY, ErrTTY and InTTY say whether each standard stream is a terminal.
	OutTTY, ErrTTY, InTTY bool
	// Color and ErrColor say whether stdout and stderr may carry ANSI.
	Color, ErrColor bool
	Width           int
	// Now is the render clock; tests pin it.
	Now    func() time.Time
	Styles Styles
	// ErrStyles is bound to the stderr renderer.
	ErrStyles Styles
	Glyphs    Glyphs
	// Cmd is the command being run (set by OutputFlags.Validate).
	Cmd string
	// Flags holds the output flags parsed so far.
	Flags FormatFlags

	env Env
}

// NewPrinter resolves the format from fl and env, then builds a Printer that
// writes to out and errw. A resolution error is an ExitInvalidArgs error.
func NewPrinter(fl FormatFlags, env Env, out, errw io.Writer) (*Printer, error) {
	if env.Getenv == nil {
		env.Getenv = func(string) string { return "" }
	}
	if env.GOOS == "" {
		env.GOOS = runtime.GOOS
	}
	if env.Width == 0 {
		env.Width = defaultWidth
	}
	res, err := ResolveFormat(fl, env.Getenv, env.OutTTY)
	if err != nil {
		return nil, err
	}
	p := &Printer{
		Format: res.Format, Legacy: res.Legacy, AgentAuto: res.Auto,
		Out: out, Err: errw, In: os.Stdin,
		OutTTY: env.OutTTY, ErrTTY: env.ErrTTY, InTTY: env.InTTY,
		Width: clampWidth(env.Width),
		Now:   time.Now,
		Flags: fl,
		env:   env,
	}
	p.Color = colorEnabled(res.Format, env.OutTTY, fl.NoColor, env.Getenv)
	p.ErrColor = colorEnabled(res.Format, env.ErrTTY, fl.NoColor, env.Getenv)

	scheme, spinner, accent := env.Scheme, env.Spinner, env.Accent
	if scheme == "" && spinner == "" && res.Format == FormatText {
		scheme, spinner, accent = loadAppearance()
	}
	p.Styles = streamStyles(out, p.Color, env.TrueColor, scheme, accent)
	p.ErrStyles = streamStyles(errw, p.ErrColor, env.TrueColor, scheme, accent)
	ascii := res.Format != FormatText || useASCII(env.Getenv, env.GOOS)
	p.Glyphs = GlyphsFor(ascii, spinner)
	return p, nil
}

// WithFlags returns a Printer for the same process and writers with fl
// applied instead of the flags this one was built with.
func (p *Printer) WithFlags(fl FormatFlags) (*Printer, error) {
	np, err := NewPrinter(fl, p.env, p.Out, p.Err)
	if err != nil {
		return nil, err
	}
	np.Now, np.Cmd, np.In = p.Now, p.Cmd, p.In
	return np, nil
}

// WithIO returns a Printer for the same flags and environment that writes to
// out and errw and asks its questions on in. Commands with injectable I/O
// (update, uninstall) use it so tests can capture every stream.
func (p *Printer) WithIO(out, errw io.Writer, in io.Reader) (*Printer, error) {
	np, err := NewPrinter(p.Flags, p.env, out, errw)
	if err != nil {
		return nil, err
	}
	np.Now, np.Cmd, np.In = p.Now, p.Cmd, in
	return np, nil
}

// EmitErr renders v in the text format on stderr: a plan shown before a
// question, which is part of the dialog and not of the result. Other formats
// print nothing.
func (p *Printer) EmitErr(v View) {
	if p.Format != FormatText {
		return
	}
	q := *p
	q.Styles = p.ErrStyles
	q.Color = p.ErrColor
	_, _ = io.WriteString(p.Err, q.renderText(v.sanitized()))
}

var (
	outMu sync.Mutex
	outP  *Printer
)

// Out returns the process Printer. Before InitOutput runs (tests, library
// use) it is built from the environment with no flags.
func Out() *Printer {
	outMu.Lock()
	defer outMu.Unlock()
	if outP == nil {
		p, err := NewPrinter(FormatFlags{}, processEnv(os.Environ()), os.Stdout, os.Stderr)
		if err != nil {
			// A bad REX_OUTPUT or REX_AGENT: InitOutput reports it; until
			// then fall back to plain output rather than failing here.
			p, _ = NewPrinter(FormatFlags{Output: "txt"}, processEnv(nil), os.Stdout, os.Stderr)
		}
		outP = p
	}
	return outP
}

// SetOut replaces the process Printer.
func SetOut(p *Printer) {
	outMu.Lock()
	outP = p
	outMu.Unlock()
}

// InitOutput strips the global output flags that precede the verb
// (-o/--output FORMAT, --agent, --no-color, -json), resolves the format from
// them and environ, and installs the process Printer. It returns the
// remaining args. On an error it still installs a plain Printer, so the
// caller can print the error.
func InitOutput(args, environ []string) ([]string, error) {
	rest, fl, err := stripGlobalFlags(args)
	if err != nil {
		SetOut(plainPrinter(environ))
		return nil, err
	}
	p, err := NewPrinter(fl, processEnv(environ), os.Stdout, os.Stderr)
	if err != nil {
		SetOut(plainPrinter(environ))
		return nil, err
	}
	SetOut(p)
	return rest, nil
}

func plainPrinter(environ []string) *Printer {
	p, _ := NewPrinter(FormatFlags{Output: "txt"}, processEnv(environ), os.Stdout, os.Stderr)
	return p
}

func processEnv(environ []string) Env {
	vars := make(map[string]string, len(environ))
	for _, kv := range environ {
		if k, v, ok := strings.Cut(kv, "="); ok {
			vars[k] = v
		}
	}
	// CLAUDECODE switches agent mode on by itself (see ResolveFormat) unless
	// the cli_agent_auto feature is off; the setting is read only when it
	// could matter.
	autoOn := sync.OnceValue(autoAgentSetting)
	getenv := func(k string) string {
		if k == "CLAUDECODE" && vars[k] != "" && !autoOn() {
			return ""
		}
		return vars[k]
	}
	return Env{
		Getenv: getenv,
		OutTTY: IsTerminal(os.Stdout), ErrTTY: IsTerminal(os.Stderr), InTTY: IsTerminal(os.Stdin),
		GOOS:  runtime.GOOS,
		Width: fileWidth(os.Stdout, getenv),
	}
}

// Verb returns the command word of args after the leading output flags
// (`rex -o json ls` gives "ls"), or "" when there is none or the flags do not
// parse. It is for code that must decide something before InitOutput runs.
func Verb(args []string) string {
	rest, _, err := stripGlobalFlags(args)
	if err != nil || len(rest) == 0 {
		return ""
	}
	return rest[0]
}

// stripGlobalFlags consumes leading output flags. It stops at the first
// argument that is not one, so `rex -o json ls` works and `rex -h` is left
// for the command dispatcher.
func stripGlobalFlags(args []string) (rest []string, fl FormatFlags, err error) {
	for len(args) > 0 {
		a := args[0]
		name, val, hasVal := strings.Cut(strings.TrimLeft(a, "-"), "=")
		if !strings.HasPrefix(a, "-") {
			break
		}
		switch name {
		case "o", "output":
			if !hasVal {
				if len(args) < 2 {
					return nil, fl, NewExitError(ExitInvalidArgs, fmt.Sprintf("flag needs an argument: %s", a))
				}
				val, args = args[1], args[1:]
			}
			fl.Output = val
		case "agent":
			fl.Output = "agent"
		case "no-color":
			fl.NoColor = true
		case "json":
			fl.Legacy = true
		default:
			return args, fl, nil
		}
		args = args[1:]
	}
	return args, fl, nil
}

// outputRegistry maps a FlagSet to its OutputFlags, so -h can list the
// output flags and the formats the command supports.
var outputRegistry sync.Map

func outputFor(fs *flag.FlagSet) *OutputFlags {
	o, _ := outputRegistry.Load(fs)
	f, _ := o.(*OutputFlags)
	return f
}

// OutputOption customizes AddOutputFlags.
type OutputOption func(*OutputFlags)

// WithHint sets the command to suggest when the format is refused: an
// interactive command names the query that gives the same information.
func WithHint(hint string) OutputOption { return func(o *OutputFlags) { o.hint = hint } }

// WithLegacyJSON registers the legacy -json flag, for commands that had one
// before versioned output. The command must set Result.Legacy.
func WithLegacyJSON() OutputOption { return func(o *OutputFlags) { o.legacy = true } }

// OutputFlags holds a command's -o/--output, --agent, --no-color and (when
// legacy) -json flags.
type OutputFlags struct {
	fl        FormatFlags
	supported FormatSet
	legacy    bool
	name      string
	hint      string
}

// AddOutputFlags registers the output flags on fs. The global flags given
// before the verb are the starting point, so a flag after the verb wins.
// After ParseArgs, call Validate.
func AddOutputFlags(fs *flag.FlagSet, supported FormatSet, opts ...OutputOption) *OutputFlags {
	o := &OutputFlags{fl: Out().Flags, supported: supported, name: fs.Name()}
	outputRegistry.Store(fs, o)
	for _, opt := range opts {
		opt(o)
	}
	setOutput := func(v string) error { o.fl.Output = v; return nil }
	fs.Func("o", "output `format`: text, txt, md, json or agent", setOutput)
	fs.Func("output", "same as -o", setOutput)
	fs.BoolFunc("agent", "one line per record, no color, no prompts (same as -o agent)",
		func(v string) error {
			on, err := strconv.ParseBool(v)
			switch {
			case err != nil:
				return err
			case on:
				o.fl.Output = "agent"
			case o.fl.Output == "agent":
				o.fl.Output = ""
			}
			return nil
		})
	fs.BoolFunc("no-color", "never emit ANSI color", func(v string) (err error) {
		o.fl.NoColor, err = strconv.ParseBool(v)
		return err
	})
	if o.legacy {
		fs.BoolFunc("json", "legacy JSON output (prefer -o json)", func(v string) (err error) {
			o.fl.Legacy, err = strconv.ParseBool(v)
			return err
		})
	}
	return o
}

// Parse is ParseArgs followed by Validate. When either fails, it first
// installs a best-effort Printer from the flags parsed so far (with Cmd set
// to the command), so main renders the error in the format the user asked
// for. Help (ErrHelpShown) leaves the Printer alone.
//
// The flag package's own output (the "flag provided but not defined" line
// and the usage block) is held back while parsing. After a parse error it is
// replayed to the FlagSet's writer only for the human formats, so agent mode
// stays exactly one stderr line and json mode only the error envelope. -h
// prints the usage in every format.
func (o *OutputFlags) Parse(fs *flag.FlagSet, args []string) ([]string, error) {
	return o.parse(fs, func() ([]string, error) { return ParseArgs(fs, args) })
}

// ParseText is ParseTextArgs followed by Validate, like Parse.
func (o *OutputFlags) ParseText(fs *flag.FlagSet, args []string, leading int) ([]string, error) {
	return o.parse(fs, func() ([]string, error) { return ParseTextArgs(fs, args, leading) })
}

func (o *OutputFlags) parse(fs *flag.FlagSet, run func() ([]string, error)) ([]string, error) {
	dest := fs.Output()
	var held bytes.Buffer
	fs.SetOutput(&held)
	pos, err := run()
	fs.SetOutput(dest)
	switch {
	case err == nil:
		err = o.Validate()
	case errors.Is(err, ErrHelpShown):
		_, _ = dest.Write(held.Bytes())
	default:
		p := o.install()
		if p.Legacy || (p.Format != FormatAgent && p.Format != FormatJSON) {
			_, _ = dest.Write(held.Bytes())
		}
	}
	if err != nil {
		return nil, err
	}
	return pos, nil
}

// install sets the process Printer from the flags parsed so far, for
// rendering an error. A flag combination that does not resolve (a conflict,
// an unknown format) drops -json, then falls back to txt.
func (o *OutputFlags) install() *Printer {
	p, ok := o.build()
	if ok {
		SetOut(p)
	}
	return p
}

// build is install without replacing the process Printer. It reports false
// when no flag combination resolved and the current Printer is returned.
func (o *OutputFlags) build() (*Printer, bool) {
	base := Out()
	for _, fl := range []FormatFlags{o.fl, {Output: o.fl.Output, NoColor: o.fl.NoColor}, {Output: "txt"}} {
		if p, err := base.WithFlags(fl); err == nil {
			p.Cmd = o.name
			return p, true
		}
	}
	return base, false
}

// Validate resolves the format from the parsed flags and the environment,
// installs the resulting Printer as the process Printer (even when it then
// rejects the flags, so the error renders in the requested format), and
// rejects a format the command does not support (ExitInvalidArgs).
func (o *OutputFlags) Validate() error {
	p := o.install()
	if _, err := ResolveFormat(o.fl, p.env.Getenv, p.env.OutTTY); err != nil {
		return err
	}
	if p.Legacy && !o.legacy {
		return NewExitError(ExitInvalidArgs, o.name+": -json is not supported")
	}
	if !o.supported.Has(p.Format) {
		return Errorf(ExitInvalidArgs, o.hint,
			"%s: output %q is not supported (supported: %s)", o.name, p.Format, o.supported)
	}
	return nil
}
