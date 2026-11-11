package main

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

// render is absent: it defines -h as the render height.
var helpCommands = []string{
	"status", "ls", "new", "attach", "reply", "send", "log", "wait", "rm",
	"prune", "rename", "name", "done", "undone", "archive", "resume", "complete",
	"reload", "completion", "setup", "doctor", "update", "uninstall",
	"digest", "stats", "fleet", "errors",
}

func TestRun_HelpFlagExitsZero(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, cmd := range helpCommands {
		t.Run(cmd, func(t *testing.T) {
			err := run([]string{cmd, "-h"})
			require.ErrorIs(t, err, core.ErrHelpShown)
			require.Equal(t, 0, exitCodeFor(err))
			require.Empty(t, err.Error(), "main prints nothing for an empty message")
		})
	}
}

func TestRun_ExplicitOutputFlagRefusedOnUnmigratedVerb(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	rest, err := core.InitOutput([]string{"-o", "json", "attach"}, nil)
	t.Cleanup(func() { core.SetOut(nil) })
	require.NoError(t, err)
	err = run(rest)
	require.Error(t, err)
	require.Equal(t, core.ExitInvalidArgs, exitCodeFor(err))
	require.Contains(t, err.Error(), "attach: interactive")

	rest, err = core.InitOutput([]string{"--agent"}, nil)
	require.NoError(t, err)
	err = run(rest)
	require.Equal(t, core.ExitInvalidArgs, exitCodeFor(err), "bare rex in agent mode has no board to show")
	require.Equal(t, "rex ls", core.Describe(err).Hint)

	_, err = core.InitOutput([]string{"--no-color", "attach"}, nil)
	require.NoError(t, err)
	require.False(t, core.Out().Flags.Explicit(), "--no-color alone is not a format request")
}

func TestRun_UnknownCommandSuggests(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, tc := range []struct{ in, want string }{
		{"lss", `did you mean "ls"`},
		{"stauts", `did you mean "stats"`},
		{"atach", `did you mean "attach"`},
		{"unistall", `did you mean "uninstall"`},
	} {
		err := run([]string{tc.in})
		require.Error(t, err, tc.in)
		require.Equal(t, core.ExitInvalidArgs, exitCodeFor(err), tc.in)
		require.Contains(t, err.Error(), tc.want)
	}

	err := run([]string{"zzzzzzzz"})
	require.Equal(t, core.ExitInvalidArgs, exitCodeFor(err))
	require.Contains(t, err.Error(), `unknown command "zzzzzzzz"`)
	require.NotContains(t, err.Error(), "did you mean")
}

// commands must list every case of run's switch, or a typo of the missing
// one gets no suggestion.
func TestRun_EveryCommandIsKnown(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "main.go", nil, 0)
	require.NoError(t, err)
	var cases []string
	ast.Inspect(f, func(n ast.Node) bool {
		fd, ok := n.(*ast.FuncDecl)
		if !ok || fd.Name.Name != "run" {
			return true
		}
		ast.Inspect(fd, func(n ast.Node) bool {
			if cc, ok := n.(*ast.CaseClause); ok {
				for _, e := range cc.List {
					if lit, ok := e.(*ast.BasicLit); ok {
						cases = append(cases, strings.Trim(lit.Value, `"`))
					}
				}
			}
			return true
		})
		return false
	})
	require.NotEmpty(t, cases)
	for _, c := range cases {
		if strings.HasPrefix(c, "-") {
			continue // --help, -h, --version, -v
		}
		require.Contains(t, commands, c, "run handles %q but commands does not list it", c)
	}
	for _, c := range commands {
		require.Contains(t, cases, c, "commands lists %q but run does not handle it", c)
	}
}

func capturePrinter(t *testing.T, format string) *bytes.Buffer {
	t.Helper()
	var out bytes.Buffer
	env := core.Env{Getenv: func(string) string { return "" }, Width: 80, Scheme: "default", GOOS: "linux"}
	// txt is the default for a non-terminal, so it needs no explicit flag.
	fl := core.FormatFlags{}
	if format != "txt" {
		fl.Output = format
	}
	p, err := core.NewPrinter(fl, env, &out, io.Discard)
	require.NoError(t, err)
	core.SetOut(p)
	t.Cleanup(func() { core.SetOut(nil) })
	return &out
}

// help, --help, agent-help and `help <command>` are routed to the kit.
func TestRun_HelpRoutes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"help"}, "# Usage"},
		{[]string{"--help"}, "# Usage"},
		{[]string{"-h", "-o", "json"}, `"commands":[`},
		{[]string{"help", "agent"}, "rex agent card v1"},
		{[]string{"agent-help"}, "rex agent card v1"},
	} {
		out := capturePrinter(t, "txt")
		require.NoError(t, run(tc.args), tc.args)
		require.Contains(t, out.String(), tc.want, tc.args)
	}
	out := capturePrinter(t, "txt")
	err := run([]string{"help", "ls"})
	require.ErrorIs(t, err, core.ErrHelpShown)
	require.Contains(t, out.String(), "Usage: rex ls [flags]")
	out = capturePrinter(t, "txt")
	require.ErrorIs(t, run([]string{"help", "new", "-o", "json"}), core.ErrHelpShown)
	require.Contains(t, out.String(), "Usage: rex new")
}

func TestRun_Completion(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, sh := range []string{"bash", "zsh", "fish"} {
		out := capturePrinter(t, "txt")
		require.NoError(t, run([]string{"completion", sh}))
		for _, c := range commands {
			require.Contains(t, out.String(), c, sh)
		}
	}
	capturePrinter(t, "txt")
	require.Equal(t, core.ExitInvalidArgs, exitCodeFor(run([]string{"completion", "tcsh"})))
}
