package core

import (
	"bytes"
	"flag"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

// parseQuiet runs OutputFlags.Parse and returns what the flag package wrote
// to the FlagSet's writer.
func parseQuiet(t *testing.T, args ...string) (usage string, err error) {
	t.Helper()
	base, perr := NewPrinter(FormatFlags{}, Env{Getenv: envOf(), Scheme: "default"}, io.Discard, io.Discard)
	require.NoError(t, perr)
	SetOut(base)
	t.Cleanup(func() { SetOut(nil) })
	var w bytes.Buffer
	fs := flag.NewFlagSet("ls", flag.ContinueOnError)
	fs.SetOutput(&w)
	fs.String("state", "", "filter by state")
	o := AddOutputFlags(fs, AllFormats, WithLegacyJSON())
	_, err = o.Parse(fs, args)
	return w.String(), err
}

func TestParseKeepsFlagUsageOutOfStructuredFormats(t *testing.T) {
	for _, args := range [][]string{
		{"--agent", "-bogus"}, {"-o", "agent", "-bogus"},
		{"-o", "json", "-bogus"}, {"-o", "json", "-state"},
	} {
		usage, err := parseQuiet(t, args...)
		require.Error(t, err)
		require.Empty(t, usage, "%v: no flag usage before the structured error", args)
	}
}

func TestParseKeepsHumanUsageOnError(t *testing.T) {
	for _, args := range [][]string{
		{"-bogus"}, {"-o", "txt", "-bogus"}, {"-o", "md", "-bogus"}, {"-o", "text", "-bogus"}, {"-json", "-bogus"},
	} {
		usage, err := parseQuiet(t, args...)
		require.Error(t, err)
		require.Contains(t, usage, "flag provided but not defined: -bogus", "%v", args)
		require.Contains(t, usage, "Usage: rex ls", "%v", args)
		require.Contains(t, usage, "rex ls -h", "%v", args)
	}
}

// -h prints the command's help to stdout, in the format asked for, and
// nothing to the FlagSet's writer.
func TestParseHelpPrintsHelpInEveryFormat(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"-h"}, "Usage: rex ls"},
		{[]string{"--agent", "-h"}, `ok cmd=help count=1 usage="rex ls`},
		{[]string{"-o", "json", "-h"}, `"command":"ls"`},
		{[]string{"-json", "-h"}, `"command":"ls"`},
		{[]string{"-o", "md", "-h"}, "| Name | Description |"},
		{[]string{"-o", "txt", "-h"}, "# Flags"},
	} {
		var out bytes.Buffer
		base, perr := NewPrinter(FormatFlags{}, Env{Getenv: envOf(), Scheme: "default"}, &out, io.Discard)
		require.NoError(t, perr)
		SetOut(base)
		t.Cleanup(func() { SetOut(nil) })
		var w bytes.Buffer
		fs := flag.NewFlagSet("ls", flag.ContinueOnError)
		fs.SetOutput(&w)
		fs.String("state", "", "filter by state")
		o := AddOutputFlags(fs, AllFormats, WithLegacyJSON())
		_, err := o.Parse(fs, tc.args)
		require.ErrorIs(t, err, ErrHelpShown)
		require.Empty(t, w.String(), "%v", tc.args)
		require.Contains(t, out.String(), tc.want, "%v", tc.args)
		require.Empty(t, Out().Cmd, "help leaves the process printer alone")
	}
}

func TestParseSuccessWritesNothing(t *testing.T) {
	usage, err := parseQuiet(t, "--agent", "-state", "done")
	require.NoError(t, err)
	require.Empty(t, usage)
}
