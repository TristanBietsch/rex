package session

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The done marker is a text/txt decoration; md, agent and json carry the
// real name.
func TestLsDoneMarkerOnlyInTextAndTXT(t *testing.T) {
	ss := lsSessions()
	for _, f := range []string{"text", "txt"} {
		require.Contains(t, emitLs(t, f, 80, false, ss), "Tests ✓done", f)
	}
	for _, f := range []string{"md", "agent", "json"} {
		out := emitLs(t, f, 80, false, ss)
		require.NotContains(t, out, "✓done", f)
		require.Contains(t, out, "Tests", f)
	}
	require.Contains(t, emitLs(t, "agent", 80, false, ss), `slug=write-tests idle_s=3600 name=Tests`)
}

// A narrow terminal never loses NAME: it moves to a dim second line.
func TestLsNarrowKeepsNameOnSecondLine(t *testing.T) {
	out := emitLs(t, "text", 40, false, lsSessions())
	lines := strings.Split(out, "\n")
	row := -1
	for i, l := range lines {
		if strings.Contains(l, "7d4f") {
			row = i
		}
	}
	require.NotEqual(t, -1, row)
	require.NotContains(t, lines[row], "Auth fix")
	require.Equal(t, "Auth fix", strings.TrimSpace(lines[row+1]))
	require.Contains(t, out, "Tests ✓done")
	require.Contains(t, out, "日本語の名前")
	// Wide enough: NAME stays in its column.
	require.Contains(t, emitLs(t, "text", 80, false, lsSessions()), "working    claude       opus · high  fix-auth-fl…    2m  Auth fix")
}
