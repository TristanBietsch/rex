package core

import (
	"flag"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseArgs(t *testing.T) {
	type want struct {
		pos    []string
		raw    bool
		socket string
		n      int
	}
	cases := []struct {
		name string
		args []string
		want want
		err  string
	}{
		{"empty", nil, want{socket: "def"}, ""},
		{"flags first", []string{"-raw", "-socket", "s", "a", "b"}, want{pos: []string{"a", "b"}, raw: true, socket: "s"}, ""},
		{"flags last", []string{"a", "b", "--raw", "--socket", "s"}, want{pos: []string{"a", "b"}, raw: true, socket: "s"}, ""},
		{"interspersed", []string{"a", "-n", "3", "b", "-raw"}, want{pos: []string{"a", "b"}, raw: true, socket: "def", n: 3}, ""},
		{"inline value", []string{"a", "-socket=x", "-raw=false"}, want{pos: []string{"a"}, socket: "x"}, ""},
		{"double dash ends flags", []string{"a", "--", "-raw", "--socket", "s"}, want{pos: []string{"a", "-raw", "--socket", "s"}, socket: "def"}, ""},
		{"flags before double dash", []string{"-raw", "a", "--", "-x"}, want{pos: []string{"a", "-x"}, raw: true, socket: "def"}, ""},
		{"lone dash is positional", []string{"-", "-raw"}, want{pos: []string{"-"}, raw: true, socket: "def"}, ""},
		{"value may look like a flag", []string{"-socket", "-raw", "a"}, want{pos: []string{"a"}, socket: "-raw"}, ""},
		{"value may be double dash", []string{"-socket", "--", "a", "-raw"}, want{pos: []string{"a"}, raw: true, socket: "--"}, ""},
		{"unknown flag after positional", []string{"a", "-clear"}, want{}, "flag provided but not defined: -clear"},
		{"missing value", []string{"a", "-socket"}, want{}, "flag needs an argument: -socket"},
		{"bad int", []string{"-n", "abc"}, want{}, "invalid value"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fs := flag.NewFlagSet("t", flag.ContinueOnError)
			fs.SetOutput(io.Discard)
			raw := fs.Bool("raw", false, "")
			socket := fs.String("socket", "def", "")
			n := fs.Int("n", 0, "")
			pos, err := ParseArgs(fs, c.args)
			if c.err != "" {
				var ee ExitError
				require.ErrorAs(t, err, &ee)
				require.Equal(t, ExitInvalidArgs, ee.Code)
				require.Contains(t, ee.Msg, c.err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, c.want.pos, pos)
			require.Equal(t, c.want.raw, *raw)
			require.Equal(t, c.want.socket, *socket)
			require.Equal(t, c.want.n, *n)
		})
	}
}

func TestCheckArgs(t *testing.T) {
	require.NoError(t, CheckArgs([]string{"a"}, 1, 1, "rex x <sel>"))
	require.NoError(t, CheckArgs(nil, 0, 0, "rex x"))
	require.NoError(t, CheckArgs([]string{"a", "b", "c"}, 1, -1, "rex x <sel> [text…]"))

	var ee ExitError
	require.ErrorAs(t, CheckArgs(nil, 1, 1, "rex x <sel>"), &ee)
	require.Equal(t, ExitInvalidArgs, ee.Code)
	require.Equal(t, "usage: rex x <sel>", ee.Msg)

	require.ErrorAs(t, CheckArgs([]string{"a", "b"}, 1, 1, "rex x <sel>"), &ee)
	require.Equal(t, ExitInvalidArgs, ee.Code)
	require.Equal(t, `unexpected argument "b" (usage: rex x <sel>)`, ee.Msg)
}
