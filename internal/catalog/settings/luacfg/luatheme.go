package luacfg

import (
	"fmt"
	"math"

	lua "github.com/yuin/gopher-lua"

	"github.com/tristanbietsch/rex/internal/catalog/theme"
)

// theme is rex.theme(name, table). The table has the YAML theme schema
// (extends, dark, colors, glyphs) and goes through theme.ParseMap, so a Lua
// theme and a theme file are held to the same rules. A refused theme is an
// Issue, not an error.
func (ev *evaluator) theme(L *lua.LState) int {
	name := L.CheckString(1)
	tbl := L.CheckTable(2)
	at := where(L)
	if len(ev.themes) >= theme.MaxFiles {
		ev.warn("%s: more than %d themes; %q ignored", at, theme.MaxFiles, name)
		return 0
	}
	raw, err := tableToMap(tbl, 1)
	if err == nil {
		var p theme.Palette
		var warns []string
		if p, warns, err = theme.ParseMap(name, raw); err == nil {
			if _, dup := ev.themes[name]; dup {
				ev.issues = append(ev.issues, theme.Issue{File: at, Msg: fmt.Sprintf("theme %q is defined again; the later definition wins", name), Warn: true})
			}
			ev.themes[name] = p
			for _, w := range warns {
				ev.issues = append(ev.issues, theme.Issue{File: at, Msg: flatten(w, maxErrRunes), Warn: true})
			}
			return 0
		}
	}
	ev.issues = append(ev.issues, theme.Issue{File: at, Msg: flatten(fmt.Sprintf("theme %q: %v", name, err), maxErrRunes)})
	return 0
}

// tableToMap converts a Lua table to the map shape theme.ParseMap reads:
// string keys become map keys, arrays become []any, whole numbers ints.
func tableToMap(t *lua.LTable, depth int) (map[string]any, error) {
	if depth > maxTableDepth {
		return nil, fmt.Errorf("tables nested more than %d deep", maxTableDepth)
	}
	out := map[string]any{}
	var err error
	t.ForEach(func(k, v lua.LValue) {
		if err != nil {
			return
		}
		if len(out) >= maxTableKeys {
			err = fmt.Errorf("a table with more than %d keys", maxTableKeys)
			return
		}
		ks, ok := k.(lua.LString)
		if !ok {
			err = fmt.Errorf("table keys must be strings")
			return
		}
		var gv any
		if gv, err = luaToAny(v, depth); err == nil {
			out[string(ks)] = gv
		}
	})
	return out, err
}

func luaToAny(v lua.LValue, depth int) (any, error) {
	switch x := v.(type) {
	case lua.LBool:
		return bool(x), nil
	case lua.LString:
		if err := checkString(string(x)); err != nil {
			return nil, err
		}
		return string(x), nil
	case lua.LNumber:
		f := float64(x)
		if f == math.Trunc(f) && math.Abs(f) < 1<<31 {
			return int(f), nil
		}
		return f, nil
	case *lua.LTable:
		if n := x.Len(); n > 0 {
			return tableToList(x, n, depth)
		}
		return tableToMap(x, depth+1)
	}
	return nil, fmt.Errorf("a %s value is not allowed in a theme", v.Type())
}

func tableToList(t *lua.LTable, n, depth int) ([]any, error) {
	if depth+1 > maxTableDepth {
		return nil, fmt.Errorf("tables nested more than %d deep", maxTableDepth)
	}
	if n > maxTableKeys {
		return nil, fmt.Errorf("a list of more than %d entries", maxTableKeys)
	}
	count := 0
	t.ForEach(func(lua.LValue, lua.LValue) { count++ })
	if count != n {
		return nil, fmt.Errorf("a table mixes list entries and keys")
	}
	out := make([]any, 0, n)
	for i := 1; i <= n; i++ {
		e, err := luaToAny(t.RawGetInt(i), depth+1)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}
