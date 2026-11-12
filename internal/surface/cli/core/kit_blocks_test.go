package core

import (
	"bytes"
	"flag"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func TestStateLabelAndTone(t *testing.T) {
	require.Equal(t, "needs you", StateLabel(protocol.StateNeedsInput))
	require.Equal(t, "working", StateLabel(protocol.StateWorking))
	for st, tone := range map[protocol.State]Tone{
		protocol.StateNeedsInput: ToneNeeds, protocol.StateWorking: ToneWorking, protocol.StateQueued: ToneQueued,
		protocol.StateDone: ToneDone, protocol.StateFailed: ToneFailed, protocol.StateCrashed: ToneCrashed,
		"nonsense": ToneNone,
	} {
		require.Equal(t, tone, StateTone(st), st)
	}
}

func TestTableTxtRecordsAndNoCount(t *testing.T) {
	v := View{
		Table{Kind: "session", TxtRecords: true, TxtFoot: "would clear 2",
			Cols: []Col{{Key: "id", Title: "ID"}, {Key: "slug", Title: "Slug"}},
			Rows: [][]Cell{{{Text: "1111"}, {Text: "a"}}, {{Text: "2222"}, {Text: "b"}}}},
		Table{Kind: "tool", NoCount: true, Cols: []Col{{Key: "tool", Title: "Tool"}}, Rows: [][]Cell{{{Text: "echo"}}}},
	}
	require.Equal(t, "1111\ta\n2222\tb\nwould clear 2\n\nTOOL\necho\n", emitKit(t, "txt", 80, false, Result{View: v}))
	agent := emitKit(t, "agent", 80, false, Result{Cmd: "x", View: v})
	require.Equal(t, "session id=1111 slug=a\nsession id=2222 slug=b\ntool tool=echo\nok cmd=x count=2\n", agent)
	only := emitKit(t, "agent", 80, false, Result{Cmd: "x", View: View{v[1]}})
	require.Equal(t, "tool tool=echo\nok cmd=x\n", only, "a view with only uncounted tables has no count")
}

func TestDefsAndParaInEveryFormat(t *testing.T) {
	long := strings.Repeat("word ", 30)
	v := View{
		Section{Title: "Flags"},
		Defs{Kind: "flag", Items: []Def{
			{Name: "-a", Desc: "short", Key: "-a", Extra: []Field{{"arg", ""}}},
			{Name: "-a-very-long-flag-name-that-overflows value", Desc: "after the name"},
			{Name: "-wrap", Desc: strings.TrimSpace(long)},
			{Name: "-bare"},
		}},
		Para{Text: "A paragraph."},
	}
	text := emitKit(t, "text", 60, false, Result{View: v})
	require.Contains(t, text, "Flags:\n  -a")
	for _, l := range strings.Split(text, "\n") {
		require.LessOrEqual(t, displayWidth(l), 60, l)
	}
	require.Contains(t, text, "  -a-very-long-flag-name-that-overflows value\n")
	require.Contains(t, text, "  -bare\n")
	txt := emitKit(t, "txt", 80, false, Result{View: v})
	require.Contains(t, txt, "# Flags\n  -a     short\n")
	require.Contains(t, txt, "  -a-very-long-flag-name-that-overflows value  after the name\n")
	require.Contains(t, txt, "  -bare\n\nA paragraph.\n")
	md := emitKit(t, "md", 80, false, Result{View: v})
	require.Contains(t, md, "| Name | Description |")
	require.Contains(t, md, "| `-a` | short |")
	require.Contains(t, md, "A paragraph.")
	agent := emitKit(t, "agent", 80, false, Result{Cmd: "help", View: v})
	require.Contains(t, agent, "flag name=-a desc=short arg=\"\"\n")
	require.NotContains(t, agent, "paragraph")
	require.Contains(t, agent, "ok cmd=help count=4")
}

func TestParseTextWiresOutputFlags(t *testing.T) {
	var out bytes.Buffer
	p, err := NewPrinter(FormatFlags{}, Env{Getenv: envOf(), Width: 80, Scheme: "default", GOOS: "linux"}, &out, io.Discard)
	require.NoError(t, err)
	SetOut(p)
	t.Cleanup(func() { SetOut(nil) })
	fs := flag.NewFlagSet("name", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	o := AddOutputFlags(fs, AllFormats)
	pos, err := o.ParseText(fs, []string{"sel", "-o", "json", "--", "-not-a-flag"}, 1)
	require.NoError(t, err)
	require.Equal(t, []string{"sel", "-not-a-flag"}, pos)
	require.Equal(t, FormatJSON, Out().Format)
	_, err = o.ParseText(fs, []string{"sel", "-bogus"}, 1)
	require.ErrorContains(t, err, "use -- before text that starts with -")
}

func TestWarnJSONShape(t *testing.T) {
	require.Equal(t, `{"v":1,"warn":{"msg":"x"}}`, warnJSON("", "x"))
}
