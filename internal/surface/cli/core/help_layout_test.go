package core

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDisplayVersion(t *testing.T) {
	for in, want := range map[string]string{
		"v2.1.0": "2.1.0",
		"2.1.0":  "2.1.0",
		"dev":    "dev",
		"":       "dev",
		"v0.0.0-20261124190851-7085c76d2335+dirty": "dev",
	} {
		require.Equal(t, want, displayVersion(in), in)
	}
}

func TestArgPlaceholder(t *testing.T) {
	require.Empty(t, argPlaceholder("trust", ""))
	require.Equal(t, "CWD", argPlaceholder("cwd", "string"))
	require.Equal(t, "DURATION", argPlaceholder("wait", "duration"))
	require.Equal(t, "NAME", argPlaceholder("skill", "NAME"))
}

// A Defs with args and no fixed widths sizes each column to its widest
// entry; a name too wide for its column puts the description on the next
// line, and an entry without a description is just name and args.
func TestDefsArgsColumnAutoWidths(t *testing.T) {
	d := Defs{Items: []Def{
		{Name: "a", Args: "<x>", Desc: "first"},
		{Name: "bb", Args: "<yy>"},
		{Name: "ccc", Desc: "third"},
	}}
	require.True(t, d.hasArgs())
	nameW, argsW := defsWidths(d)
	require.Equal(t, 3, nameW)
	require.Equal(t, 4, argsW)

	out := emitKit(t, "text", 80, false, Result{Cmd: "x", View: View{d}})
	require.Equal(t, "  a   <x>   first\n  bb  <yy>\n  ccc       third\n", out)

	wide := Defs{Items: []Def{{Name: "much-too-long", Args: "<x>", Desc: "below"}}, NameWidth: 4, ArgsWidth: 3}
	out = emitKit(t, "text", 80, false, Result{Cmd: "x", View: View{wide}})
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	require.Len(t, lines, 2)
	require.Equal(t, "  much-too-long <x>", lines[0])
	require.Equal(t, strings.Repeat(" ", 2+4+1+3+2)+"below", lines[1])

	require.False(t, Defs{Items: []Def{{Name: "a", Desc: "b"}}}.hasArgs())
	require.True(t, Defs{Items: []Def{{Name: "a"}}, NameWidth: 2}.hasArgs())
}
