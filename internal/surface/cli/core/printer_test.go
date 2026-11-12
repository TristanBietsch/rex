package core

import (
	"bytes"
	"errors"
	"flag"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/theme"
)

func envOf(kv ...string) func(string) string {
	m := map[string]string{}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	return func(k string) string { return m[k] }
}

func TestResolveFormat(t *testing.T) {
	tests := []struct {
		name    string
		fl      FormatFlags
		env     []string
		tty     bool
		want    Format
		legacy  bool
		auto    bool
		wantErr string
	}{
		{name: "tty default is text", tty: true, want: FormatText},
		{name: "pipe default is txt", want: FormatTXT},
		{name: "-o json", fl: FormatFlags{Output: "json"}, tty: true, want: FormatJSON},
		{name: "-o md in pipe", fl: FormatFlags{Output: "md"}, want: FormatMD},
		{name: "-o text in pipe", fl: FormatFlags{Output: "text"}, want: FormatText},
		{name: "--agent", fl: FormatFlags{Output: "agent"}, tty: true, want: FormatAgent},
		{name: "-json is legacy json", fl: FormatFlags{Legacy: true}, want: FormatJSON, legacy: true},
		{name: "-json with -o conflicts", fl: FormatFlags{Legacy: true, Output: "txt"}, wantErr: "conflicting output flags"},
		{name: "-json with --agent conflicts", fl: FormatFlags{Legacy: true, Output: "agent"}, wantErr: "conflicting output flags"},
		{name: "unknown format", fl: FormatFlags{Output: "yaml"}, wantErr: `unknown output format "yaml"`},
		{name: "flag beats REX_AGENT", fl: FormatFlags{Output: "md"}, env: []string{"REX_AGENT", "1"}, want: FormatMD},
		{name: "flag beats REX_OUTPUT", fl: FormatFlags{Output: "md"}, env: []string{"REX_OUTPUT", "json"}, want: FormatMD},
		{name: "REX_AGENT=1", env: []string{"REX_AGENT", "1"}, tty: true, want: FormatAgent},
		{name: "REX_AGENT beats REX_OUTPUT", env: []string{"REX_AGENT", "1", "REX_OUTPUT", "json"}, want: FormatAgent},
		{name: "REX_OUTPUT", env: []string{"REX_OUTPUT", "md"}, want: FormatMD},
		{name: "REX_OUTPUT invalid", env: []string{"REX_OUTPUT", "yaml"}, wantErr: "REX_OUTPUT"},
		{name: "REX_AGENT invalid", env: []string{"REX_AGENT", "maybe"}, wantErr: "REX_AGENT"},
		{name: "REX_AGENT=0 keeps default", env: []string{"REX_AGENT", "0"}, want: FormatTXT},
		{name: "CLAUDECODE without tty is agent", env: []string{"CLAUDECODE", "1"}, want: FormatAgent, auto: true},
		{name: "CLAUDECODE with tty is text", env: []string{"CLAUDECODE", "1"}, tty: true, want: FormatText},
		{name: "REX_AGENT=0 beats CLAUDECODE", env: []string{"CLAUDECODE", "1", "REX_AGENT", "0"}, want: FormatTXT},
		{name: "REX_OUTPUT beats CLAUDECODE", env: []string{"CLAUDECODE", "1", "REX_OUTPUT", "json"}, want: FormatJSON},
		{name: "flag beats CLAUDECODE", fl: FormatFlags{Output: "txt"}, env: []string{"CLAUDECODE", "1"}, want: FormatTXT},
		{name: "CLAUDECODE=0 is not agent", env: []string{"CLAUDECODE", "0"}, want: FormatTXT},
		{name: "explicit REX_AGENT=1 is not auto", env: []string{"CLAUDECODE", "1", "REX_AGENT", "1"}, want: FormatAgent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveFormat(tt.fl, envOf(tt.env...), tt.tty)
			if tt.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErr)
				var ee ExitError
				require.True(t, errors.As(err, &ee))
				require.Equal(t, ExitInvalidArgs, ee.Code)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got.Format)
			require.Equal(t, tt.legacy, got.Legacy)
			require.Equal(t, tt.auto, got.Auto)
		})
	}
}

func TestFormatSet(t *testing.T) {
	require.True(t, AllFormats.Has(FormatAgent))
	require.False(t, TextFormats.Has(FormatJSON))
	require.Equal(t, "text, txt", TextFormats.String())
	require.Equal(t, "text, txt, md, json, agent", AllFormats.String())
	require.Equal(t, "json", FormatJSON.String())
	require.Contains(t, Format(99).String(), "99")
}

func TestColorDecision(t *testing.T) {
	tests := []struct {
		name    string
		f       Format
		tty     bool
		noColor bool
		env     []string
		want    bool
	}{
		{name: "tty text", f: FormatText, tty: true, want: true},
		{name: "pipe text", f: FormatText},
		{name: "NO_COLOR set", f: FormatText, tty: true, env: []string{"NO_COLOR", "1"}},
		{name: "NO_COLOR empty is ignored", f: FormatText, tty: true, env: []string{"NO_COLOR", ""}, want: true},
		{name: "TERM=dumb", f: FormatText, tty: true, env: []string{"TERM", "dumb"}},
		{name: "--no-color", f: FormatText, tty: true, noColor: true},
		{name: "CLICOLOR_FORCE in pipe", f: FormatText, env: []string{"CLICOLOR_FORCE", "1"}, want: true},
		{name: "CLICOLOR_FORCE=0", f: FormatText, env: []string{"CLICOLOR_FORCE", "0"}},
		{name: "CLICOLOR_FORCE beats TERM=dumb", f: FormatText, env: []string{"CLICOLOR_FORCE", "1", "TERM", "dumb"}, want: true},
		{name: "NO_COLOR beats CLICOLOR_FORCE", f: FormatText, env: []string{"CLICOLOR_FORCE", "1", "NO_COLOR", "1"}},
		{name: "--no-color beats CLICOLOR_FORCE", f: FormatText, noColor: true, env: []string{"CLICOLOR_FORCE", "1"}},
		{name: "txt never colored", f: FormatTXT, tty: true, env: []string{"CLICOLOR_FORCE", "1"}},
		{name: "md never colored", f: FormatMD, tty: true, env: []string{"CLICOLOR_FORCE", "1"}},
		{name: "json never colored", f: FormatJSON, tty: true, env: []string{"CLICOLOR_FORCE", "1"}},
		{name: "agent never colored", f: FormatAgent, tty: true, env: []string{"CLICOLOR_FORCE", "1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, colorEnabled(tt.f, tt.tty, tt.noColor, envOf(tt.env...)))
		})
	}
}

func newTestPrinter(t *testing.T, fl FormatFlags, env Env) (*Printer, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	var out, errw bytes.Buffer
	if env.Scheme == "" {
		env.Scheme = "default"
	}
	p, err := NewPrinter(fl, env, &out, &errw)
	require.NoError(t, err)
	return p, &out, &errw
}

func TestPrinterStreamsDecideColorIndependently(t *testing.T) {
	env := Env{Getenv: envOf(), OutTTY: false, ErrTTY: true, TrueColor: true}
	p, _, _ := newTestPrinter(t, FormatFlags{Output: "text"}, env)
	require.False(t, p.Color)
	require.True(t, p.ErrColor)
	require.NotContains(t, p.Styles.Err.Render("x"), "\x1b")
	require.Contains(t, p.ErrStyles.Err.Render("x"), "\x1b[")

	env = Env{Getenv: envOf(), OutTTY: true, ErrTTY: false, TrueColor: true}
	p, _, _ = newTestPrinter(t, FormatFlags{Output: "text"}, env)
	require.True(t, p.Color)
	require.False(t, p.ErrColor)
	require.Contains(t, p.Styles.Err.Render("x"), "\x1b[")
	require.NotContains(t, p.ErrStyles.Err.Render("x"), "\x1b", "stderr must not follow stdout's TTY state")
}

func TestPrinterNoANSIOutsideText(t *testing.T) {
	for _, f := range []string{"txt", "md", "json", "agent"} {
		env := Env{Getenv: envOf("CLICOLOR_FORCE", "1"), OutTTY: true, ErrTTY: true, TrueColor: true}
		p, _, _ := newTestPrinter(t, FormatFlags{Output: f}, env)
		require.False(t, p.Color, f)
		require.NotContains(t, p.Styles.Accent.Render("x"), "\x1b", f)
		require.NotContains(t, p.ErrStyles.Err.Render("x"), "\x1b", f)
	}
}

func TestPrinterForcedColorInPipeUsesANSI256(t *testing.T) {
	p, _, _ := newTestPrinter(t, FormatFlags{Output: "text"},
		Env{Getenv: envOf("CLICOLOR_FORCE", "1")})
	require.True(t, p.Color)
	require.Contains(t, p.Styles.OK.Render("x"), "\x1b[38;5;")
}

func TestCLIStylesFollowTheme(t *testing.T) {
	env := Env{Getenv: envOf(), OutTTY: true, TrueColor: true, Scheme: "paper"}
	p, _, _ := newTestPrinter(t, FormatFlags{Output: "text"}, env)
	// paper Fg #1C1D21, Danger #C23B3B
	require.Contains(t, p.Styles.Bold.Render("x"), "38;2;28;29;32")
	require.Contains(t, p.Styles.Err.Render("x"), "38;2;194;59;59")

	env.Scheme = "default"
	p, _, _ = newTestPrinter(t, FormatFlags{Output: "text"}, env)
	require.Contains(t, p.Styles.Bold.Render("x"), "38;2;230;232;238")

	env.Scheme = "no-such-scheme"
	p, _, _ = newTestPrinter(t, FormatFlags{Output: "text"}, env)
	require.Contains(t, p.Styles.Bold.Render("x"), "38;2;230;232;238", "unknown scheme falls back to default")
}

func TestPrinterReadsSchemeFromSettings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home+"/.config")
	require.NoError(t, os.MkdirAll(home+"/.config/rex", 0o700))
	require.NoError(t, os.WriteFile(home+"/.config/rex/config.yaml", []byte("theme: paper\nspinner: moon\n"), 0o600))

	var out, errw bytes.Buffer
	p, err := NewPrinter(FormatFlags{Output: "text"}, Env{Getenv: envOf(), OutTTY: true, TrueColor: true}, &out, &errw)
	require.NoError(t, err)
	require.Contains(t, p.Styles.Bold.Render("x"), "38;2;28;29;32")
	require.Equal(t, "◐", p.Glyphs.State["working"])

	// Non-text formats never read settings or color.
	p, err = NewPrinter(FormatFlags{Output: "txt"}, Env{Getenv: envOf(), OutTTY: true}, &out, &errw)
	require.NoError(t, err)
	require.Equal(t, "*", p.Glyphs.State["working"])
}

func TestGlyphs(t *testing.T) {
	u := GlyphsFor(false, "")
	require.Equal(t, "⠋", u.State["working"])
	require.Equal(t, "◆", u.State["needs_input"])
	require.Equal(t, "⠋", GlyphsFor(false, "bogus").State["working"])
	a := GlyphsFor(true, "moon")
	all := []string{a.OK, a.Warn, a.Fail, a.Info, a.Hint, a.Sep}
	for _, g := range a.State {
		all = append(all, g)
	}
	for _, g := range all {
		for _, r := range g {
			require.Less(t, r, rune(0x80), "ascii set has %q", g)
		}
	}
	require.Len(t, a.State, 6)
}

func TestASCIISelection(t *testing.T) {
	tests := []struct {
		name string
		env  []string
		goos string
		want bool
	}{
		{name: "unix default", goos: "linux"},
		{name: "REX_ASCII", env: []string{"REX_ASCII", "1"}, goos: "linux", want: true},
		{name: "windows legacy console", goos: "windows", want: true},
		{name: "windows terminal", env: []string{"WT_SESSION", "x"}, goos: "windows"},
		{name: "windows term program", env: []string{"TERM_PROGRAM", "vscode"}, goos: "windows"},
	}
	for _, tt := range tests {
		require.Equal(t, tt.want, useASCII(envOf(tt.env...), tt.goos), tt.name)
	}
	p, _, _ := newTestPrinter(t, FormatFlags{Output: "text"},
		Env{Getenv: envOf(), GOOS: "windows", OutTTY: true})
	require.Equal(t, "v", p.Glyphs.OK)
	p, _, _ = newTestPrinter(t, FormatFlags{Output: "text"}, Env{Getenv: envOf(), GOOS: "linux", OutTTY: true})
	require.Equal(t, "✓", p.Glyphs.OK)
	p, _, _ = newTestPrinter(t, FormatFlags{Output: "txt"}, Env{Getenv: envOf(), GOOS: "linux", OutTTY: true})
	require.Equal(t, "v", p.Glyphs.OK, "txt always uses ASCII")
}

func TestWidthClamp(t *testing.T) {
	require.Equal(t, 40, clampWidth(10))
	require.Equal(t, 200, clampWidth(999))
	require.Equal(t, 100, clampWidth(100))
	f, err := os.CreateTemp(t.TempDir(), "w")
	require.NoError(t, err)
	defer f.Close()
	require.Equal(t, 80, fileWidth(f, envOf()))
	require.Equal(t, 120, fileWidth(f, envOf("COLUMNS", "120")))
	require.Equal(t, 40, fileWidth(f, envOf("COLUMNS", "5")))
	require.Equal(t, 80, fileWidth(f, envOf("COLUMNS", "junk")))
	require.False(t, IsTerminal(f))
	require.False(t, IsTerminal(nil))

	p, _, _ := newTestPrinter(t, FormatFlags{Output: "text"}, Env{Getenv: envOf(), Width: 500})
	require.Equal(t, 200, p.Width)
	p, _, _ = newTestPrinter(t, FormatFlags{Output: "text"}, Env{Getenv: envOf()})
	require.Equal(t, 80, p.Width)
}

func TestEnableVTRestoreNoopOffConsole(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "vt")
	require.NoError(t, err)
	defer f.Close()
	restore := EnableVT(f, nil)
	restore()
}

func TestStripGlobalFlags(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantRest []string
		wantFl   FormatFlags
		wantErr  bool
	}{
		{name: "none", args: []string{"ls", "-state", "done"}, wantRest: []string{"ls", "-state", "done"}},
		{name: "-o value", args: []string{"-o", "json", "ls"}, wantRest: []string{"ls"}, wantFl: FormatFlags{Output: "json"}},
		{name: "--output=value", args: []string{"--output=md", "ls"}, wantRest: []string{"ls"}, wantFl: FormatFlags{Output: "md"}},
		{name: "--agent", args: []string{"--agent", "status"}, wantRest: []string{"status"}, wantFl: FormatFlags{Output: "agent"}},
		{name: "last wins", args: []string{"-o", "json", "--agent", "ls"}, wantRest: []string{"ls"}, wantFl: FormatFlags{Output: "agent"}},
		{name: "-json and no-color", args: []string{"-json", "--no-color", "ls"}, wantRest: []string{"ls"}, wantFl: FormatFlags{Legacy: true, NoColor: true}},
		{name: "stops at unknown flag", args: []string{"-h"}, wantRest: []string{"-h"}},
		{name: "stops at verb flags", args: []string{"ls", "-o", "json"}, wantRest: []string{"ls", "-o", "json"}},
		{name: "missing value", args: []string{"-o"}, wantErr: true},
		{name: "empty", args: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rest, fl, err := stripGlobalFlags(tt.args)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantRest, rest)
			require.Equal(t, tt.wantFl, fl)
		})
	}
}

func TestInitOutput(t *testing.T) {
	t.Cleanup(func() { SetOut(nil) })
	rest, err := InitOutput([]string{"-o", "json", "ls"}, []string{"HOME=/x"})
	require.NoError(t, err)
	require.Equal(t, []string{"ls"}, rest)
	require.Equal(t, FormatJSON, Out().Format)
	require.True(t, Out().Flags.Explicit())

	_, err = InitOutput([]string{"-o", "yaml", "ls"}, nil)
	require.Error(t, err)
	require.Equal(t, FormatTXT, Out().Format, "a plain printer is installed on error")

	_, err = InitOutput([]string{"-o"}, nil)
	require.Error(t, err)

	_, err = InitOutput([]string{"ls"}, []string{"REX_OUTPUT=bogus"})
	require.Error(t, err)
}

func TestOutDefaultsWithoutInit(t *testing.T) {
	SetOut(nil)
	t.Cleanup(func() { SetOut(nil) })
	require.NotNil(t, Out())
	t.Setenv("REX_OUTPUT", "bogus")
	SetOut(nil)
	require.Equal(t, FormatTXT, Out().Format)
}

func TestOutputFlags(t *testing.T) {
	setup := func(t *testing.T, env ...string) {
		t.Helper()
		p, err := NewPrinter(FormatFlags{}, Env{Getenv: envOf(env...), Scheme: "default"}, io.Discard, io.Discard)
		require.NoError(t, err)
		SetOut(p)
		t.Cleanup(func() { SetOut(nil) })
	}
	parse := func(args ...string) (*OutputFlags, error) {
		fs := flag.NewFlagSet("ls", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		o := AddOutputFlags(fs, AllFormats, WithLegacyJSON())
		_, err := ParseArgs(fs, args)
		return o, err
	}

	t.Run("flag anywhere", func(t *testing.T) {
		setup(t)
		o, err := parse("x", "-o", "json")
		require.NoError(t, err)
		require.NoError(t, o.Validate())
		require.Equal(t, FormatJSON, Out().Format)
		require.Equal(t, "ls", Out().Cmd)
	})
	t.Run("agent and no-color", func(t *testing.T) {
		setup(t)
		o, err := parse("--agent", "--no-color")
		require.NoError(t, err)
		require.NoError(t, o.Validate())
		require.Equal(t, FormatAgent, Out().Format)
		require.True(t, Out().Flags.NoColor)
	})
	t.Run("legacy json", func(t *testing.T) {
		setup(t)
		o, err := parse("-json")
		require.NoError(t, err)
		require.NoError(t, o.Validate())
		require.True(t, Out().Legacy)
	})
	t.Run("conflict", func(t *testing.T) {
		setup(t)
		o, err := parse("-json", "--agent")
		require.NoError(t, err)
		require.ErrorContains(t, o.Validate(), "conflicting output flags")
	})
	t.Run("leading flags are the base, later wins", func(t *testing.T) {
		p, err := NewPrinter(FormatFlags{Output: "md"}, Env{Getenv: envOf()}, io.Discard, io.Discard)
		require.NoError(t, err)
		SetOut(p)
		t.Cleanup(func() { SetOut(nil) })
		o, err := parse()
		require.NoError(t, err)
		require.NoError(t, o.Validate())
		require.Equal(t, FormatMD, Out().Format)
		o, err = parse("-o", "txt")
		require.NoError(t, err)
		require.NoError(t, o.Validate())
		require.Equal(t, FormatTXT, Out().Format)
	})
	t.Run("unsupported format", func(t *testing.T) {
		setup(t)
		fs := flag.NewFlagSet("attach", flag.ContinueOnError)
		o := AddOutputFlags(fs, SetOf(FormatText))
		_, err := ParseArgs(fs, []string{"-o", "json"})
		require.NoError(t, err)
		err = o.Validate()
		require.EqualError(t, err, `attach: output "json" is not supported (supported: text)`)
	})
	t.Run("legacy json not registered", func(t *testing.T) {
		setup(t)
		fs := flag.NewFlagSet("new", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		AddOutputFlags(fs, AllFormats)
		_, err := ParseArgs(fs, []string{"-json"})
		var ee ExitError
		require.ErrorAs(t, err, &ee)
		require.Equal(t, ExitInvalidArgs, ee.Code)
	})
	t.Run("leading -json on a command without legacy", func(t *testing.T) {
		p, err := NewPrinter(FormatFlags{Legacy: true}, Env{Getenv: envOf()}, io.Discard, io.Discard)
		require.NoError(t, err)
		SetOut(p)
		t.Cleanup(func() { SetOut(nil) })
		fs := flag.NewFlagSet("new", flag.ContinueOnError)
		o := AddOutputFlags(fs, AllFormats)
		_, err = ParseArgs(fs, nil)
		require.NoError(t, err)
		require.ErrorContains(t, o.Validate(), "-json is not supported")
	})
	t.Run("REX_OUTPUT applies without flags", func(t *testing.T) {
		setup(t, "REX_OUTPUT", "md")
		o, err := parse()
		require.NoError(t, err)
		require.NoError(t, o.Validate())
		require.Equal(t, FormatMD, Out().Format)
	})
	t.Run("help lists the flags", func(t *testing.T) {
		setup(t)
		fs := flag.NewFlagSet("ls", flag.ContinueOnError)
		var usage strings.Builder
		fs.SetOutput(&usage)
		AddOutputFlags(fs, AllFormats)
		fs.PrintDefaults()
		require.Contains(t, usage.String(), "-o format")
		require.Contains(t, usage.String(), "-agent")
	})
}

// The ansi scheme has no color for its dim tones: they are the terminal
// default drawn faint (index 8 would vanish on Solarized Dark).
func TestCLIAnsiSchemeDimIsFaint(t *testing.T) {
	r := lipgloss.NewRenderer(io.Discard)
	r.SetColorProfile(termenv.TrueColor)
	st := NewStyles(theme.Builtin["ansi"], r)
	for name, s := range map[string]lipgloss.Style{"dim": st.Dim, "muted": st.Muted, "hr": st.HR} {
		require.Contains(t, s.Render("x"), "\x1b[2m", name)
	}
	st = NewStyles(theme.Builtin["default"], r)
	require.NotContains(t, st.Dim.Render("x"), "\x1b[2m")
}
