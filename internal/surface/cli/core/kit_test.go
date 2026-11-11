package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/testutil/golden"
)

// kitView exercises every block type. Tables carry text-only, txt-only and
// agent-only columns.
func kitView() View {
	return View{
		Header{Title: "Sessions", Meta: "1 needs you · 1 working"},
		Section{Title: "Fleet"},
		KV{Kind: "info", Pairs: []Pair{
			{Key: "version", Label: "Version", Val: Cell{Text: "v1"}},
			{Key: "path", Label: "Binary", Val: Cell{Text: "/very/long/path/to/the/rex/binary/that/needs/to/wrap/around/the/terminal/width/for/sure/ok"}},
		}},
		Table{Kind: "session", Cols: []Col{
			{Key: "mark", Title: "", Formats: SetOf(FormatText)},
			{Key: "id", Title: "ID", Width: 5},
			{Key: "state", Title: "State", Width: 12},
			{Key: "slug", Title: "Slug", Flex: true, Max: 24, Width: 24},
			{Key: "n", Title: "N", Align: AlignRight, Width: 3},
			{Key: "name", Title: "Name"},
		}, Rows: [][]Cell{
			{{Text: "◆", Tone: ToneNeeds}, {Text: "a91c"}, {Text: "needs you", Txt: "needs_input", Raw: "needs_input", Tone: ToneNeeds}, {Text: "refactor-db-with-a-really-long-slug-name"}, {Text: "7", Raw: 7}, {Text: ""}},
			{{Text: "●", Tone: ToneDone}, {Text: "0b2e"}, {Text: "done", Raw: "done", Tone: ToneDone}, {Text: "日本語のスラッグ"}, {Text: "12", Raw: 12}, {Text: "a|b `c` <i>"}},
		}},
		Checks{Rows: []Check{
			{Name: "rex binary", Status: "pass", Detail: "~/.local/bin/rex"},
			{Name: "daemon", Status: "warn", Detail: "not responding", Hint: "rex daemon start"},
			{Name: "config", Status: "fail", Detail: "bad"},
			{Name: "extra", Status: "info", Detail: "n/a"},
		}},
		Note{Tone: ToneWarn, Text: "on-disk snapshot"},
		Line{Tone: ToneOK, Glyph: "✓", Text: "started 7d4f", Txt: "7d4f\tfix"},
		Hint{Cmd: "rex attach 7d4f", Text: "open a session"},
	}
}

func goldenPrinter(t *testing.T, format string, width int, color bool) (*Printer, *bytes.Buffer) {
	t.Helper()
	var out bytes.Buffer
	env := Env{Getenv: envOf(), OutTTY: color, ErrTTY: color, Width: width, Scheme: "default", TrueColor: true, GOOS: "linux"}
	if color {
		env.Getenv = envOf()
	}
	p, err := NewPrinter(FormatFlags{Output: format}, env, &out, io.Discard)
	require.NoError(t, err)
	p.Cmd = "kit"
	return p, &out
}

func emitKit(t *testing.T, format string, width int, color bool, r Result) string {
	t.Helper()
	p, out := goldenPrinter(t, format, width, color)
	require.NoError(t, p.Emit(r))
	return out.String()
}

func TestKitGolden(t *testing.T) {
	r := Result{Cmd: "kit", View: kitView(), Meta: map[string]any{"source": "disk", "a": 1},
		Data: map[string]any{"sessions": []int{1, 2}, "html": "<b>"}}
	var secs []golden.Section
	for _, w := range []int{40, 80, 120} {
		secs = append(secs, golden.Section{Name: fmt.Sprintf("text.w%d", w), Body: emitKit(t, "text", w, false, r)})
	}
	secs = append(secs,
		golden.Section{Name: "text.color.w80", Body: emitKit(t, "text", 80, true, r)},
		golden.Section{Name: "txt", Body: emitKit(t, "txt", 80, false, r)},
		golden.Section{Name: "md", Body: emitKit(t, "md", 80, false, r)},
		golden.Section{Name: "json", Body: emitKit(t, "json", 80, false, r)},
		golden.Section{Name: "agent", Body: emitKit(t, "agent", 80, false, r)},
	)
	golden.Check(t, "testdata/golden/kit.txtar", secs...)
}

func TestKitTextFitsWidth(t *testing.T) {
	for _, w := range []int{40, 80, 120} {
		out := emitKit(t, "text", w, false, Result{View: kitView()})
		for _, l := range strings.Split(out, "\n") {
			if strings.HasPrefix(l, "  ") && strings.Contains(l, "refactor") || strings.Contains(l, "ID") {
				require.LessOrEqual(t, ansi.StringWidth(l), w, l)
			}
		}
		require.False(t, strings.HasSuffix(out, "\n\n"), "no trailing blank line")
		require.False(t, strings.HasPrefix(out, "\n"), "no blank line on top")
	}
}

func TestKitNoANSIWhenNotColor(t *testing.T) {
	for _, f := range []string{"text", "txt", "md", "json", "agent"} {
		out := emitKit(t, f, 80, false, Result{Cmd: "kit", View: kitView()})
		require.NotContains(t, out, "\x1b", f)
	}
	for _, f := range []string{"txt", "md", "json", "agent"} {
		var out bytes.Buffer
		p, err := NewPrinter(FormatFlags{Output: f},
			Env{Getenv: envOf("CLICOLOR_FORCE", "1"), OutTTY: true, ErrTTY: true, Scheme: "default", TrueColor: true}, &out, io.Discard)
		require.NoError(t, err)
		require.NoError(t, p.Emit(Result{Cmd: "kit", View: kitView()}))
		require.NotContains(t, out.String(), "\x1b", f)
	}
}

func TestKitTXTHasNoHeaderOrHint(t *testing.T) {
	out := emitKit(t, "txt", 80, false, Result{View: kitView()})
	require.NotContains(t, out, "Sessions")
	require.NotContains(t, out, "open a session")
	require.Contains(t, out, "# Fleet")
	require.Contains(t, out, "7d4f\tfix")
	for _, l := range strings.Split(out, "\n") {
		require.True(t, utf8.ValidString(l))
	}
}

func TestKitTXTTableColumns(t *testing.T) {
	out := emitKit(t, "txt", 80, false, Result{View: View{kitView()[3]}})
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	require.Equal(t, "ID     STATE         SLUG                        N  NAME", lines[0])
	require.True(t, strings.HasPrefix(lines[1], "a91c   needs_input   refactor-db-with-a-real…"), lines[1])
	require.NotContains(t, lines[0], " MARK")
}

func TestKitEmptyTable(t *testing.T) {
	v := View{Table{Kind: "session", Empty: "(no sessions)", Cols: []Col{{Key: "id", Title: "ID"}}}}
	for _, f := range []string{"text", "txt", "md"} {
		require.Contains(t, emitKit(t, f, 80, false, Result{View: v}), "(no sessions)", f)
	}
	require.Equal(t, "ok cmd=kit count=0\n", emitKit(t, "agent", 80, false, Result{Cmd: "kit", View: v}))
}

func TestKitAgentShape(t *testing.T) {
	out := emitKit(t, "agent", 80, false, Result{Cmd: "kit", View: kitView(), Meta: map[string]any{"source": "disk"}})
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	require.True(t, strings.HasPrefix(lines[0], "info version=v1 path=/very/long/path"), lines[0])
	require.Contains(t, lines, `session id=a91c state=needs_input slug=refactor-db-with-a-really-long-slug-name n=7 name=""`)
	require.Contains(t, lines, `check name="rex binary" status=pass detail="~/.local/bin/rex"`)
	require.Equal(t, "ok cmd=kit count=2 source=disk", lines[len(lines)-1])
	for _, l := range lines {
		require.NotContains(t, l, "\x1b")
	}
}

func TestKitAgentKVTrailer(t *testing.T) {
	v := View{KV{Pairs: []Pair{{Key: "needs_input", Val: Cell{Raw: 1}}, {Key: "done", Val: Cell{Raw: 2}}}}}
	require.Equal(t, "ok cmd=status needs_input=1 done=2\n", emitKit(t, "agent", 80, false, Result{Cmd: "status", View: v}))
}

func TestKitInlineKV(t *testing.T) {
	v := View{KV{Inline: true, Txt: "1 awaiting input · 2 working", Pairs: []Pair{
		{Key: "needs_input", Label: "needs you", Glyph: "◆", Val: Cell{Text: "1", Tone: ToneNeeds}},
		{Key: "working", Label: "working", Glyph: "⠋", Val: Cell{Text: "2", Tone: ToneWorking}},
	}}}
	require.Equal(t, "◆ 1 needs you   ⠋ 2 working\n", emitKit(t, "text", 80, false, Result{View: v}))
	require.Equal(t, "1 awaiting input · 2 working\n", emitKit(t, "txt", 80, false, Result{View: v}))
}

func TestKitMarkdownEscape(t *testing.T) {
	require.Equal(t, `a\|b \`+"`"+`c\`+"`"+` &lt;script&gt;`, escapeMD("a|b `c` <script>"))
	require.Equal(t, `\# not a heading`, escapeMD("# not a heading"))
	require.Equal(t, `a\\b`, escapeMD(`a\b`))
	// Links, images, emphasis and strikethrough cannot be injected.
	require.Equal(t, `\!\[x\](http://e)`, escapeMD("![x](http://e)"))
	require.Equal(t, `\[click\]`, escapeMD("[click]"))
	require.Equal(t, `\*b\* \_i\_ \~s\~ hi\!`, escapeMD("*b* _i_ ~s~ hi!"))
	out := emitKit(t, "md", 80, false, Result{View: kitView()})
	require.Contains(t, out, `a\|b \`+"`"+`c\`+"`"+` &lt;i&gt;`)
	require.Contains(t, out, "| ID | State | Slug | N | Name |")
	require.Contains(t, out, "| --- | --- | --- | ---: | --- |")
	require.Contains(t, out, "### Sessions")
	require.Contains(t, out, "> on-disk snapshot")
}

func TestKitJSONEnvelopeV1(t *testing.T) {
	out := emitKit(t, "json", 80, false, Result{Cmd: "ls", Data: []string{"<a>"}, Meta: map[string]any{"source": "disk"}})
	require.Equal(t, `{"v":1,"cmd":"ls","data":["<a>"],"meta":{"source":"disk"}}`+"\n", out)
	out = emitKit(t, "json", 80, false, Result{Cmd: "ls", Data: map[string]int{"n": 1}})
	require.Equal(t, `{"v":1,"cmd":"ls","data":{"n":1}}`+"\n", out)

	p, _ := goldenPrinter(t, "json", 80, false)
	err := p.Emit(Result{Cmd: "x", Data: make(chan int)})
	require.Error(t, err)
}

func TestKitLegacy(t *testing.T) {
	var out bytes.Buffer
	p, err := NewPrinter(FormatFlags{Legacy: true}, Env{Getenv: envOf(), Scheme: "default"}, &out, io.Discard)
	require.NoError(t, err)
	require.NoError(t, p.Emit(Result{Cmd: "ls", Legacy: func(w io.Writer) error { _, err := io.WriteString(w, "legacy\n"); return err }}))
	require.Equal(t, "legacy\n", out.String())

	err = p.Emit(Result{Cmd: "ls"})
	var ee ExitError
	require.ErrorAs(t, err, &ee)
	require.Equal(t, ExitInvalidArgs, ee.Code)
	require.Empty(t, out.String()[len("legacy\n"):])
}

func TestKitFlattenAllCells(t *testing.T) {
	evil := "x\x1b]0;pwn\a\x1b[31my\nz\u202e"
	v := View{
		Header{Title: evil, Meta: evil},
		Section{Title: evil},
		KV{Kind: "k", Pairs: []Pair{{Key: "a", Label: evil, Val: Cell{Text: evil, Txt: evil}}}},
		Table{Kind: "t", Cols: []Col{{Key: "a", Title: "A"}}, Rows: [][]Cell{{{Text: evil, Txt: evil}}}},
		Checks{Rows: []Check{{Name: evil, Status: "pass", Detail: evil, Hint: evil}}},
		Note{Text: evil}, Hint{Cmd: evil, Text: evil}, Line{Text: evil, Txt: evil},
	}
	for _, f := range []string{"text", "txt", "md", "json", "agent"} {
		out := emitKit(t, f, 80, false, Result{Cmd: "kit", View: v, Data: evil})
		require.NotContains(t, out, "\x1b", f)
		if f != "json" {
			require.NotContains(t, out, "\u202e", f)
		}
		require.NotContains(t, out, "\a", f)
		if f == "text" || f == "txt" || f == "md" {
			require.NotContains(t, out, "pwn\a", f)
		}
	}
	// A slug that is hostile in agent mode stays one line.
	out := emitKit(t, "agent", 80, false, Result{Cmd: "kit", View: v})
	for _, l := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
		require.NotContains(t, l, "\n")
	}
}

func TestEncodeLine(t *testing.T) {
	ts := time.Date(2026, 1, 2, 3, 4, 5, 0, time.FixedZone("x", 3600))
	require.Equal(t,
		`session id=a91c name="" msg="hi there" n=3 ok=true t=2026-01-02T02:04:05Z p=/a/b@c+d%e:f.g-h_i`,
		EncodeLine("session", Field{"id", "a91c"}, Field{"name", ""}, Field{"msg", "hi there"},
			Field{"n", 3}, Field{"ok", true}, Field{"t", ts}, Field{"p", "/a/b@c+d%e:f.g-h_i"}))
	require.Equal(t, "x v=\"a\\x1bb\" w=\"a\\nb\" u=\"a\\u202eb\"",
		EncodeLine("x", Field{"v", "a\x1bb"}, Field{"w", "a\nb"}, Field{"u", "a\u202eb"}))
	require.Equal(t, `x a=1 b=2 c=nil_ e=""`,
		EncodeLine("x", Field{"a", int64(1)}, Field{"b", uint64(2)}, Field{"c", "nil_"}, Field{"e", nil}))
	require.Equal(t, `x d=1s`, EncodeLine("x", Field{"d", time.Second}))
	require.Equal(t, `x f=1.5`, EncodeLine("x", Field{"f", 1.5}))
	require.Panics(t, func() { EncodeLine("Bad") })
	require.Panics(t, func() { EncodeLine("ok", Field{"Bad-Key", 1}) })
	require.Panics(t, func() { EncodeLine("") })
}

// parseLine is the inverse of EncodeLine for string values.
func parseLine(t testing.TB, line string) (kind string, vals []string, ok bool) {
	kind, rest, _ := strings.Cut(line, " ")
	for rest != "" {
		_, v, found := strings.Cut(rest, "=")
		if !found {
			return "", nil, false
		}
		if strings.HasPrefix(v, `"`) {
			end := 1
			for end < len(v) && v[end] != '"' {
				if v[end] == '\\' {
					end++
				}
				end++
			}
			if end >= len(v) {
				return "", nil, false
			}
			s, err := strconv.Unquote(v[:end+1])
			if err != nil {
				return "", nil, false
			}
			vals = append(vals, s)
			rest = strings.TrimPrefix(v[end+1:], " ")
		} else {
			bare, r, _ := strings.Cut(v, " ")
			vals = append(vals, bare)
			rest = r
		}
	}
	return kind, vals, true
}

func FuzzAgentLine(f *testing.F) {
	for _, s := range []string{"", "plain", "a b", "a\nb", "\x1b[31m", "q\"uote", "\\", "\u202e", "é", "日本", "a=b", "\x00", "\xff"} {
		f.Add(s, s)
	}
	f.Fuzz(func(t *testing.T, a, b string) {
		line := EncodeLine("rec", Field{"a", a}, Field{"b", b})
		require.NotContains(t, line, "\n")
		require.NotContains(t, line, "\r")
		require.NotContains(t, line, "\x1b")
		require.True(t, utf8.ValidString(line))
		kind, vals, ok := parseLine(t, line)
		require.True(t, ok, line)
		require.Equal(t, "rec", kind)
		if utf8.ValidString(a) && utf8.ValidString(b) {
			require.Equal(t, []string{a, b}, vals)
		}
	})
}

func TestErrorsStructured(t *testing.T) {
	cause := errors.New("dial unix /x/rex.sock: connect: no such file or directory")
	err := Errorf(ExitDaemonUnreachable, "rex daemon start", "rex-daemon is not running: %w", cause)
	require.ErrorIs(t, err, cause)
	require.Equal(t, "rex daemon start", err.Hint)
	require.Equal(t, "rex-daemon is not running: "+cause.Error(), err.Error())
	require.Equal(t, "daemon_unreachable", Kind(err.Code))

	plain := Errorf(ExitInvalidArgs, "", "bad %s", "thing")
	require.NoError(t, plain.Err)

	kinds := map[int]string{0: "ok", 1: "error", 2: "not_found", 3: "ambiguous", 4: "daemon_unreachable",
		5: "invalid_args", 6: "refused", 7: "timeout", 99: "error"}
	for c, k := range kinds {
		require.Equal(t, k, Kind(c))
	}
	require.True(t, IsSilent(nil))
	require.True(t, IsSilent(ErrHelpShown))
	require.True(t, IsSilent(NewExitError(ExitGeneric, "")))
	require.False(t, IsSilent(err))
	require.ErrorIs(t, ErrHelpShown, ErrHelpShown)
}

func TestDescribe(t *testing.T) {
	info := Describe(errors.New("boom"))
	require.Equal(t, ErrorInfo{Code: 1, Kind: "error", Msg: "boom"}, info)

	cause := errors.New("no such file")
	info = Describe(fmt.Errorf("wrap: %w", Errorf(4, "rex daemon start", "daemon down (see below): %w", cause)))
	require.Equal(t, 4, info.Code)
	require.Equal(t, "rex daemon start", info.Hint)
	require.Empty(t, info.Cause, "cause already in the message")

	info = Describe(ExitError{Code: 4, Msg: "daemon down", Err: cause})
	require.Equal(t, "no such file", info.Cause)

	info = Describe(codeErr{})
	require.Equal(t, 9, info.Code)
}

type codeErr struct{}

func (codeErr) Error() string { return "x" }
func (codeErr) ExitCode() int { return 9 }

func TestRenderError(t *testing.T) {
	cause := errors.New("no socket at /x/rex.sock")
	err := ExitError{Code: 4, Msg: "rex-daemon is not running", Hint: "rex daemon start", Err: cause}
	render := func(fl FormatFlags, env Env, cmd string, e error) string {
		var errw bytes.Buffer
		env.Scheme, env.TrueColor = "default", true
		if env.Getenv == nil {
			env.Getenv = envOf()
		}
		p, perr := NewPrinter(fl, env, io.Discard, &errw)
		require.NoError(t, perr)
		p.Cmd = cmd
		p.RenderError(e)
		return errw.String()
	}
	secs := []golden.Section{
		{Name: "text.color", Body: render(FormatFlags{Output: "text"}, Env{ErrTTY: true, OutTTY: true}, "ls", err)},
		{Name: "text", Body: render(FormatFlags{Output: "text"}, Env{}, "ls", err)},
		{Name: "txt", Body: render(FormatFlags{Output: "txt"}, Env{}, "ls", err)},
		{Name: "md", Body: render(FormatFlags{Output: "md"}, Env{}, "ls", err)},
		{Name: "agent", Body: render(FormatFlags{Output: "agent"}, Env{}, "ls", err)},
		{Name: "json", Body: render(FormatFlags{Output: "json"}, Env{}, "ls", err)},
		{Name: "legacy", Body: render(FormatFlags{Legacy: true}, Env{}, "ls", err)},
	}
	golden.Check(t, "testdata/golden/error.txtar", secs...)

	require.Equal(t, "rex: plain\n", render(FormatFlags{Output: "txt"}, Env{}, "", errors.New("plain")))
	require.Equal(t, "error code=1 kind=error msg=plain\n", render(FormatFlags{Output: "agent"}, Env{}, "", errors.New("plain")))
	require.Empty(t, render(FormatFlags{Output: "agent"}, Env{}, "ls", ErrHelpShown))
	require.Empty(t, render(FormatFlags{Output: "txt"}, Env{}, "status", NewExitError(1, "")))

	// JSON error is valid, versioned, and carries the kind.
	var doc struct {
		V     int `json:"v"`
		Error struct {
			Code int
			Kind string
			Cmd  string
			Msg  string
			Hint string
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal([]byte(render(FormatFlags{Output: "json"}, Env{}, "ls", err)), &doc))
	require.Equal(t, 1, doc.V)
	require.Equal(t, "daemon_unreachable", doc.Error.Kind)
	require.Equal(t, "ls", doc.Error.Cmd)

	// Hostile message cannot escape in any format.
	evil := errors.New("bad\x1b]0;x\a\nline2")
	for _, f := range []string{"text", "txt", "md", "agent", "json"} {
		out := render(FormatFlags{Output: f}, Env{}, "ls", evil)
		require.NotContains(t, out, "\x1b", f)
		want := 2 // plain formats keep the message's line break
		if f == "agent" || f == "json" {
			want = 1
		}
		require.Equal(t, want, strings.Count(out, "\n"), f)
	}

	// Multi-line errors (captured command output) print as before.
	multi := errors.New("update failed:\nfatal: not a repository\n  hint: run it in a repo")
	for _, f := range []FormatFlags{{Output: "txt"}, {Output: "md"}, {Legacy: true}} {
		require.Equal(t, "rex: "+multi.Error()+"\n", render(f, Env{}, "update", multi))
	}
	require.Contains(t, render(FormatFlags{Output: "text"}, Env{}, "update", multi), "fatal: not a repository\n")
	require.Equal(t, 1, strings.Count(render(FormatFlags{Output: "agent"}, Env{}, "update", multi), "\n"))
}

func TestJSONErrorStdoutEmpty(t *testing.T) {
	var out, errw bytes.Buffer
	p, err := NewPrinter(FormatFlags{Output: "json"}, Env{Getenv: envOf(), Scheme: "default"}, &out, &errw)
	require.NoError(t, err)
	p.RenderError(NewExitError(ExitSelectorNotFound, "no such session"))
	require.Empty(t, out.String())
	require.Contains(t, errw.String(), `"kind":"not_found"`)
}

func TestOutputFlagsFailurePathRendersInRequestedFormat(t *testing.T) {
	run := func(t *testing.T, args ...string) string {
		t.Helper()
		var errw bytes.Buffer
		base, err := NewPrinter(FormatFlags{}, Env{Getenv: envOf(), Scheme: "default"}, io.Discard, &errw)
		require.NoError(t, err)
		SetOut(base)
		t.Cleanup(func() { SetOut(nil) })
		fs := flag.NewFlagSet("ls", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		o := AddOutputFlags(fs, AllFormats, WithLegacyJSON())
		_, err = o.Parse(fs, args)
		require.Error(t, err)
		Out().RenderError(err)
		return errw.String()
	}
	require.Regexp(t, `^error code=5 kind=invalid_args cmd=ls msg="flag provided but not defined: -bogus"`+"\n$",
		run(t, "--agent", "-bogus"))
	require.Regexp(t, `^\{"v":1,"error":\{"code":5,"kind":"invalid_args","cmd":"ls","msg":"flag provided but not defined: -bogus"\}\}`+"\n$",
		run(t, "-o", "json", "-bogus"))
	require.Regexp(t, `^error code=5 kind=invalid_args cmd=ls msg="conflicting output flags`,
		run(t, "-json", "--agent"))
	require.Regexp(t, `^rex: unknown output format "yaml"`, run(t, "-o", "yaml"))
	require.Contains(t, run(t, "-o", "json", "-o", "yaml", "-bogus"), "rex: flag provided but not defined",
		"an unresolvable format falls back to txt")
	require.Contains(t, run(t, "-o", "md", "x", "-agent=maybe"), "invalid", "a bad bool value is a parse error")

	// Help leaves the printer alone and stays silent.
	SetOut(nil)
	t.Cleanup(func() { SetOut(nil) })
	fs := flag.NewFlagSet("ls", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	o := AddOutputFlags(fs, AllFormats)
	_, err := o.Parse(fs, []string{"-h"})
	require.ErrorIs(t, err, ErrHelpShown)
	require.Empty(t, Out().Cmd)
}

func TestOutputFlagsBoolValues(t *testing.T) {
	parse := func(base FormatFlags, args ...string) FormatFlags {
		p, err := NewPrinter(base, Env{Getenv: envOf(), Scheme: "default"}, io.Discard, io.Discard)
		require.NoError(t, err)
		SetOut(p)
		t.Cleanup(func() { SetOut(nil) })
		fs := flag.NewFlagSet("ls", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		o := AddOutputFlags(fs, AllFormats, WithLegacyJSON())
		_, err = ParseArgs(fs, args)
		require.NoError(t, err)
		return o.fl
	}
	require.Equal(t, FormatFlags{}, parse(FormatFlags{}, "-json=false", "--agent=false", "-no-color=false"))
	require.Equal(t, FormatFlags{Legacy: true, NoColor: true}, parse(FormatFlags{}, "-json=true", "-no-color"))
	require.Equal(t, FormatFlags{Output: "agent"}, parse(FormatFlags{}, "--agent=true"))
	require.Equal(t, FormatFlags{}, parse(FormatFlags{Output: "agent"}, "--agent=false"), "=false undoes a leading --agent")
	require.Equal(t, FormatFlags{Output: "json"}, parse(FormatFlags{Output: "json"}, "--agent=false"), "=false leaves other formats")
	require.Equal(t, FormatFlags{}, parse(FormatFlags{Legacy: true, NoColor: true}, "-json=0", "-no-color=f"))
}
