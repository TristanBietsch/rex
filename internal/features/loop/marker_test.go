package loop

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func markerDef(t *testing.T, marker string) Until {
	t.Helper()
	d, err := Parse([]byte("name: x\nkind: repeat\ntool: claude\nmodel: sonnet\nprompt: go\nuntil:\n  marker: '"+marker+"'\n"), "x")
	require.NoError(t, err)
	return d.Until
}

// The marker only matches a line by itself; quoting it in a sentence does
// not stop the loop.
func TestMatchMarker_WholeLineOnly(t *testing.T) {
	u := markerDef(t, "<promise>DONE</promise>")
	require.True(t, u.MatchMarker("work\n<promise>DONE</promise>\n"))
	require.True(t, u.MatchMarker("work\n   <promise>DONE</promise>   \r\nmore"), "surrounding whitespace is ignored")
	require.False(t, u.MatchMarker("I will print <promise>DONE</promise> when finished"))
	require.False(t, u.MatchMarker(`say "<promise>DONE</promise>" to stop`))
	require.False(t, u.MatchMarker("<promise>DONE</promise> and more"))
	require.False(t, u.MatchMarker(""))
}

func TestMatchMarker_RegexAndAlternation(t *testing.T) {
	u := markerDef(t, "ALL DONE|STOP")
	require.True(t, u.MatchMarker("ALL DONE"))
	require.True(t, u.MatchMarker("STOP"))
	require.False(t, u.MatchMarker("ALL DONE now"), "alternation stays anchored as a group")
	require.False(t, u.MatchMarker("please STOP"))
}

func TestMatchMarker_NoMarker(t *testing.T) {
	d, err := Parse([]byte(goodYAML), "fix")
	require.NoError(t, err)
	require.False(t, d.Until.MatchMarker("anything"))
}

func TestCompile_RestoresMarker(t *testing.T) {
	d := Def{Until: Until{Marker: "DONE"}}
	require.False(t, d.Until.MatchMarker("DONE"))
	require.NoError(t, d.Compile())
	require.True(t, d.Until.MatchMarker("DONE"))
	bad := Def{Until: Until{Marker: "("}}
	require.Error(t, bad.Compile())
}
