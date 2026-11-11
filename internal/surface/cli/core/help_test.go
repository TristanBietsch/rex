package core

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/testutil/golden"
)

func helpResult() Result {
	return Result{Cmd: "help", Data: helpData{Version: Version, Commands: commandsData()}, View: helpView()}
}

func TestHelpGolden(t *testing.T) {
	r := helpResult()
	var secs []golden.Section
	for _, w := range []int{40, 80, 120} {
		secs = append(secs, golden.Section{Name: fmt.Sprintf("text.w%d", w), Body: emitKit(t, "text", w, false, r)})
	}
	secs = append(secs,
		golden.Section{Name: "text.color.w80", Body: emitKit(t, "text", 80, true, r)},
		golden.Section{Name: "txt", Body: emitKit(t, "txt", 80, false, r)},
		golden.Section{Name: "md", Body: emitKit(t, "md", 80, false, r)},
		golden.Section{Name: "json", Body: emitKit(t, "json", 80, false, r)},
		golden.Section{Name: "agent.lines", Body: emitKit(t, "agent", 80, false, Result{Cmd: "help", View: helpView()})},
	)
	golden.Check(t, "testdata/golden/help.txtar", secs...)
}

func TestAgentCardGolden(t *testing.T) {
	r := agentCardResult()
	var secs []golden.Section
	for _, f := range []string{"text", "txt", "md", "json", "agent"} {
		secs = append(secs, golden.Section{Name: f, Body: emitKit(t, f, 80, false, r)})
	}
	golden.Check(t, "testdata/golden/agent_help.txtar", secs...)
}

// The card stays small enough to read in one go: at most 30 lines and about
// 1.2k tokens (4 characters each).
func TestAgentCardIsCompact(t *testing.T) {
	lines := strings.Split(strings.TrimSuffix(agentCard, "\n"), "\n")
	require.LessOrEqual(t, len(lines), 30)
	require.LessOrEqual(t, len(agentCard), 4800)
	require.NotContains(t, agentCard, "\x1b")
	// agent mode: the card is the output, with no ok trailer.
	out := emitKit(t, "agent", 80, false, agentCardResult())
	require.Equal(t, agentCard, out)
}

func TestAgentCardJSONHasCommandsAndKinds(t *testing.T) {
	out := emitKit(t, "json", 80, false, agentCardResult())
	require.Contains(t, out, `"kinds":["ok","error","not_found","ambiguous","daemon_unreachable","invalid_args","refused","timeout"]`)
	require.Contains(t, out, `"name":"resume"`)
	require.Contains(t, out, `"4":"daemon_unreachable"`)
}

func TestHelpTextFitsWidth(t *testing.T) {
	for _, w := range []int{40, 80, 120} {
		for i, l := range strings.Split(emitKit(t, "text", w, false, helpResult()), "\n") {
			require.LessOrEqual(t, displayWidth(l), w, "width %d line %d: %q", w, i, l)
		}
	}
}

// Agy layout: Usage first, Title-case sections with a colon, no banner, no
// rule, no blank line at the top, one closing hint.
func TestHelpLayout(t *testing.T) {
	out := emitKit(t, "text", 80, false, helpResult())
	lines := strings.Split(out, "\n")
	require.True(t, strings.HasPrefix(lines[0], "rex — Runtime Executive for Agents"), lines[0])
	require.Equal(t, "Usage:", lines[2])
	for _, sec := range append(append([]string{}, HelpSections...), "Output") {
		require.Contains(t, lines, sec+":")
	}
	require.NotContains(t, out, "─")
	require.NotContains(t, out, "✦")
	require.NotContains(t, out, "USAGE")
	require.Contains(t, out, `Run "rex <command> -h" for flags · agents: rex agent-help`)
	require.NotContains(t, out, "new [flags] -- <prompt>   start a session (wizard)")
	require.True(t, strings.HasSuffix(out, "agent-help\n"))
}

// Every table entry is well formed, so help, the card and completion agree.
func TestCommandsTable(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range Commands {
		require.NotEmpty(t, c.Name)
		require.False(t, seen[c.Name], c.Name)
		seen[c.Name] = true
		require.Contains(t, HelpSections, c.Section, c.Name)
		require.NotEmpty(t, c.Short, c.Name)
		require.NotEmpty(t, c.Example, c.Name)
		if c.Name == "new" {
			require.NotContains(t, c.Short+c.Long, "wizard", "new takes flags; it is not a wizard")
		}
		for _, a := range c.Aliases {
			got, ok := FindCommand(a)
			require.True(t, ok)
			require.Equal(t, c.Name, got.Name)
		}
	}
	require.Contains(t, CommandNames(), "help")
	_, ok := FindCommand("frobnicate")
	require.False(t, ok)
}

func cmdHelpFlagSet(out io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet("ls", flag.ContinueOnError)
	fs.SetOutput(out)
	AddSocketFlag(fs)
	fs.String("state", "", "filter by state")
	fs.Bool("wide", false, "add columns")
	fs.Duration("timeout", 0, "max `wait` time")
	fs.Int("n", 5, "lines")
	AddOutputFlags(fs, AllFormats, WithLegacyJSON())
	return fs
}

func TestCommandHelpGolden(t *testing.T) {
	var secs []golden.Section
	for _, f := range []string{"text", "txt", "md", "json", "agent"} {
		var out bytes.Buffer
		p, err := NewPrinter(FormatFlags{Output: f}, Env{Getenv: envOf(), Width: 80, Scheme: "default", GOOS: "linux"}, &out, io.Discard)
		require.NoError(t, err)
		SetOut(p)
		t.Cleanup(func() { SetOut(nil) })
		_, err = ParseArgs(cmdHelpFlagSet(io.Discard), []string{"-h"})
		require.ErrorIs(t, err, ErrHelpShown)
		secs = append(secs, golden.Section{Name: f, Body: out.String()})
	}
	golden.Check(t, "testdata/golden/command_help.txtar", secs...)
}

// A flag error gets a one-line usage hint, not the whole flag list.
func TestFlagErrorPrintsOneLineUsage(t *testing.T) {
	var w bytes.Buffer
	_, err := ParseArgs(cmdHelpFlagSet(&w), []string{"-bogus"})
	require.Error(t, err)
	require.Equal(t, "flag provided but not defined: -bogus\nUsage: rex ls [flags]  (run `rex ls -h` for the flags)\n", w.String())
}

func TestRunHelpTopics(t *testing.T) {
	for _, tc := range []struct {
		args []string
		fmt  string
		want string
	}{
		{nil, "txt", "Selectors: short id"},
		{[]string{"agent"}, "txt", "rex agent card v1"},
		{nil, "agent", "rex agent card v1"},
		{[]string{"-o", "json"}, "txt", `"commands":[`},
	} {
		var out bytes.Buffer
		p, err := NewPrinter(FormatFlags{Output: tc.fmt}, Env{Getenv: envOf(), Width: 80, Scheme: "default", GOOS: "linux"}, &out, io.Discard)
		require.NoError(t, err)
		SetOut(p)
		t.Cleanup(func() { SetOut(nil) })
		require.NoError(t, RunHelp(tc.args))
		require.Contains(t, out.String(), tc.want, "%v", tc.args)
	}
	code := func(err error) int {
		var ec ExitCoder
		require.True(t, errors.As(err, &ec), "%v", err)
		return ec.ExitCode()
	}
	require.Equal(t, ExitInvalidArgs, code(RunHelp([]string{"nope"})))
	require.Equal(t, ExitInvalidArgs, code(RunHelp([]string{"a", "b"})))
	require.Equal(t, ExitInvalidArgs, code(RunAgentHelp([]string{"x"})))
	SetOut(nil)
}
