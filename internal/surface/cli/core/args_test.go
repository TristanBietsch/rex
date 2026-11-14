package core

import (
	"errors"
	"flag"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

func newTestFS() (*flag.FlagSet, *string, *bool) {
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	s := fs.String("tool", "", "")
	b := fs.Bool("v", false, "")
	return fs, s, b
}

func TestParseArgs(t *testing.T) {
	cases := []struct {
		name string
		args []string
		pos  []string
		tool string
		v    bool
	}{
		{"flags first", []string{"-tool", "x", "a", "b"}, []string{"a", "b"}, "x", false},
		{"flags after positionals", []string{"a", "-tool", "x", "b", "-v"}, []string{"a", "b"}, "x", true},
		{"equals form", []string{"a", "-tool=y"}, []string{"a"}, "y", false},
		{"double dash", []string{"a", "--", "-tool", "x"}, []string{"a", "-tool", "x"}, "", false},
		{"bare dash is positional", []string{"-", "a"}, []string{"-", "a"}, "", false},
		{"windows path value", []string{"a", "-tool", `C:\x\y`}, []string{"a"}, `C:\x\y`, false},
		{"empty", nil, nil, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fs, tool, v := newTestFS()
			pos, err := ParseArgs(fs, c.args)
			require.NoError(t, err)
			require.Equal(t, c.pos, pos)
			require.Equal(t, c.tool, *tool)
			require.Equal(t, c.v, *v)
		})
	}
}

func TestParseArgs_UnknownFlagExit5(t *testing.T) {
	fs, _, _ := newTestFS()
	_, err := ParseArgs(fs, []string{"a", "-nope"})
	var ec ExitCoder
	require.True(t, errors.As(err, &ec))
	require.Equal(t, ExitInvalidArgs, ec.ExitCode())
}

func TestParseArgs_HelpIsErrHelpShown(t *testing.T) {
	fs, _, _ := newTestFS()
	_, err := ParseArgs(fs, []string{"a", "-h"})
	require.ErrorIs(t, err, ErrHelpShown)
	var ec ExitCoder
	require.True(t, errors.As(err, &ec))
	require.Equal(t, 0, ec.ExitCode())
	require.Empty(t, err.Error())
}

func TestParseTextArgs(t *testing.T) {
	fs, tool, _ := newTestFS()
	pos, err := ParseTextArgs(fs, []string{"hello", "world", "-tool", "claude"}, 0)
	require.NoError(t, err)
	require.Equal(t, []string{"hello", "world"}, pos)
	require.Equal(t, "claude", *tool)

	fs, _, _ = newTestFS()
	pos, err = ParseTextArgs(fs, []string{"sel", "--", "-x", "text"}, 1)
	require.NoError(t, err)
	require.Equal(t, []string{"sel", "-x", "text"}, pos)
}

func TestParseTextArgs_UnknownFlagAfterTextHints(t *testing.T) {
	fs, _, _ := newTestFS()
	_, err := ParseTextArgs(fs, []string{"sel", "fix", "-x"}, 1)
	require.ErrorContains(t, err, "use -- before text that starts with -")
	var ec ExitCoder
	require.True(t, errors.As(err, &ec))
	require.Equal(t, ExitInvalidArgs, ec.ExitCode())

	// Before the text begins there is no hint: it is just a bad flag.
	fs, _, _ = newTestFS()
	_, err = ParseTextArgs(fs, []string{"-x", "sel", "t"}, 1)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "use --")
}
