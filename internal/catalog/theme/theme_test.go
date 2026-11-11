package theme

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEveryBuiltinComplete(t *testing.T) {
	for _, name := range Names() {
		p := Builtin[name]
		require.Equal(t, name, p.Name)
		v := reflect.ValueOf(p)
		for i := range v.NumField() {
			f := v.Type().Field(i)
			switch f.Type.Kind() {
			case reflect.String:
				require.NotEmpty(t, v.Field(i).String(), "%s.%s is empty", name, f.Name)
			case reflect.Slice:
				require.NotZero(t, v.Field(i).Len(), "%s.%s is empty", name, f.Name)
			default:
			}
		}
	}
}

func TestNamesSortedAndKnown(t *testing.T) {
	require.Equal(t, []string{"classic", "default", "noir", "paper"}, Names())
}

func TestDefaultIsClassic(t *testing.T) {
	d, c := Builtin["default"], Builtin["classic"]
	d.Name, c.Name = "", ""
	require.Equal(t, c, d)
}

func TestDarkFlags(t *testing.T) {
	require.True(t, Builtin["default"].Dark)
	require.True(t, Builtin["noir"].Dark)
	require.False(t, Builtin["paper"].Dark)
}

func TestSemanticAliasesMatchStates(t *testing.T) {
	for _, name := range Names() {
		p := Builtin[name]
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
