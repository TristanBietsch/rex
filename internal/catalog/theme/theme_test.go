package theme

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

// ansi leaves the neutral tones to the terminal default.
var ansiNeutral = map[string]bool{"Bg": true, "Fg": true, "FgDim": true, "FgMuted": true, "BgSel": true, "Border": true, "BorderFocus": true, "Crashed": true}

func TestEveryBuiltinComplete(t *testing.T) {
	for _, name := range Names() {
		p, ok := Get(name)
		require.True(t, ok, name)
		require.Equal(t, name, p.Name)
		v := reflect.ValueOf(p)
		for i := range v.NumField() {
			f := v.Type().Field(i)
			switch f.Type.Kind() {
			case reflect.String:
				// ansi leaves text, background and selection to the terminal.
				if name == "ansi" && ansiNeutral[f.Name] {
					continue
				}
				require.NotEmpty(t, v.Field(i).String(), "%s.%s is empty", name, f.Name)
			case reflect.Slice:
				require.NotZero(t, v.Field(i).Len(), "%s.%s is empty", name, f.Name)
			default:
			}
		}
	}
}

func TestNamesSortedAndKnown(t *testing.T) {
	require.Equal(t, []string{"ansi", "classic", "default", "dracula", "high-contrast", "noir", "pantone_safe", "paper", "tokyo_night"}, Names())
}

func TestDefaultIsVioletAndClassicKeepsTheOldLook(t *testing.T) {
	require.Equal(t, "#B392F0", Builtin["default"].Accent)
	require.Equal(t, "#5B8DEF", Builtin["classic"].Accent)
	require.Equal(t, Builtin["classic"].Working, Builtin["classic"].Accent, "classic paints the accent like working, as it always did")
	require.NotEqual(t, Builtin["default"].Working, Builtin["default"].Accent, "the accent is not a state color")
}

func TestDarkFlags(t *testing.T) {
	require.True(t, Builtin["default"].Dark)
	require.True(t, Builtin["noir"].Dark)
	require.True(t, Builtin["high-contrast"].Dark)
	require.False(t, Builtin["paper"].Dark)
}

func TestSemanticAliasesMatchStates(t *testing.T) {
	for _, name := range Names() {
		p, _ := Get(name)
		require.Equal(t, p.Needs, p.Warn, name)
		require.Equal(t, p.Failed, p.Danger, name)
		require.Equal(t, p.Done, p.Success, name)
		require.Equal(t, p.Working, p.Info, name)
	}
}

func TestGet(t *testing.T) {
	p, ok := Get("paper")
	require.True(t, ok)
	require.Equal(t, "paper", p.Name)

	p, ok = Get("nope")
	require.False(t, ok)
	require.Equal(t, "default", p.Name)
}

func TestGetReturnsIndependentFleet(t *testing.T) {
	p, _ := Get("default")
	p.Fleet[0] = "mutated"
	q, _ := Get("default")
	require.NotEqual(t, "mutated", q.Fleet[0])
}
