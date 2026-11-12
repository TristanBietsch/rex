package tui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

// Every name in the command table dispatches, and none is listed twice.
func TestCommandTableParity(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range commands {
		require.NotEmpty(t, c.Names, "command without names")
		require.NotEmpty(t, c.Desc, "%v: help text", c.Names)
		require.NotEmpty(t, c.Group, "%v: group", c.Names)
		require.NotNil(t, c.Run, "%v: Run", c.Names)
		for _, n := range c.Names {
			require.False(t, seen[n], "name %q listed twice", n)
			seen[n] = true
			m := commandModel(t)
			m, _ = executeCommand(m, n)
			require.NotContains(t, m.Err, "unknown command", ":%s must dispatch", n)
		}
	}
}

func TestUnknownCommandNamesHelp(t *testing.T) {
	m, _ := executeCommand(commandModel(t), "nonsense")
	require.Contains(t, m.Err, "unknown command: nonsense")
	require.Contains(t, m.Err, ":help")
}

// The help overlay lists every command (key column and description).
func TestHelpListsEveryCommand(t *testing.T) {
	rows := map[string]string{}
	for _, g := range helpRight {
		for _, r := range g.Rows {
			rows[r.Key] = r.Desc
		}
	}
	for _, c := range commands {
		require.Equal(t, c.Desc, rows[c.helpKey()], "help row for %v", c.Names)
	}
}

func TestHelpLineKeysHaveLabels(t *testing.T) {
	for _, k := range helpLineKeys {
		require.NotEmpty(t, helpLineLabel(k), "help line key %q has no Line label", k)
	}
}

// updateKeyLiterals collects every string literal that appears in a case
// clause of update.go: the keys updateKey and its helpers switch on.
func updateKeyLiterals(t *testing.T) map[string]bool {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "update.go", nil, 0)
	require.NoError(t, err)
	lits := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		cc, ok := n.(*ast.CaseClause)
		if !ok {
			return true
		}
		for _, e := range cc.List {
			if bl, ok := e.(*ast.BasicLit); ok && bl.Kind == token.STRING {
				if s, err := strconv.Unquote(bl.Value); err == nil {
					lits[s] = true
				}
			}
		}
		return true
	})
	return lits
}

// Bindings are descriptive: this catches a documented board key that no
// handler switches on any more.
func TestBindingsAreHandled(t *testing.T) {
	lits := updateKeyLiterals(t)
	for _, b := range bindings {
		require.NotEmpty(t, b.Keys)
		require.NotEmpty(t, b.Desc, "binding %q", b.Keys)
		require.NotZero(t, b.Ctx, "binding %q", b.Keys)
		if b.Ctx&ctxBoard == 0 || len(b.Press) == 0 {
			continue
		}
		if h, ok := boardChords[b.Press[0]]; ok && len(b.Press) == 2 {
			// A chord: first key arms it, second completes it.
			m, _ := chordModel()
			_, _, done := h(m, b.Press[1])
			require.True(t, done, "binding %q: chord not completed by %q", b.Keys, b.Press[1])
			continue
		}
		for _, k := range b.Press {
			require.True(t, lits[k], "binding %q: update.go has no case for key %q", b.Keys, k)
		}
	}
}
