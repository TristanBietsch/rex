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
		require.Contains(t, usage, "Usage of ls:", "%v", args)
		require.Contains(t, usage, "-state", "%v", args)
	}
}

func TestParseHelpPrintsUsageInEveryFormat(t *testing.T) {
	for _, args := range [][]string{{"-h"}, {"--agent", "-h"}, {"-o", "json", "-h"}, {"-o", "txt", "-h"}} {
		usage, err := parseQuiet(t, args...)
		require.ErrorIs(t, err, ErrHelpShown)
		require.Contains(t, usage, "Usage of ls:", "%v", args)
	}
}

func TestParseSuccessWritesNothing(t *testing.T) {
	usage, err := parseQuiet(t, "--agent", "-state", "done")
	require.NoError(t, err)
	require.Empty(t, usage)
}
