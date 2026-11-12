package luacfg

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync/atomic"
	"unicode"
	"unicode/utf8"

	lua "github.com/yuin/gopher-lua"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/catalog/theme"
	"github.com/tristanbietsch/rex/internal/runtime/privfile"
)

// moduleName is what require accepts: dotted lower-case words. It cannot
// hold a slash, a dot-dot or a drive letter, so a name always maps to a file
// below the module directory.
var moduleName = regexp.MustCompile(`^[a-z0-9_]+(\.[a-z0-9_]+)*$`)

// evaluator holds the state of one evaluation. It is owned by one goroutine;
// Eval reads it only through the Result that goroutine sends.
type evaluator struct {
	opts    Opts
	heap    atomic.Bool // the heap watchdog stopped the script
	values  map[string]any
	setAt   map[string]string
	themes  map[string]theme.Palette
	issues  []theme.Issue
	warns   []string
	nWarn   int
	loaded  map[string]lua.LValue
	loading map[string]bool
	total   int // source bytes read so far
}

func newEvaluator(o Opts, mainSize int) *evaluator {
	return &evaluator{
		opts:    o,
		values:  map[string]any{},
		setAt:   map[string]string{},
		themes:  map[string]theme.Palette{},
		loaded:  map[string]lua.LValue{},
		loading: map[string]bool{},
		total:   mainSize,
	}
}

func (ev *evaluator) warn(format string, args ...any) {
	if ev.nWarn++; ev.nWarn <= MaxWarnings {
		ev.warns = append(ev.warns, flatten(fmt.Sprintf(format, args...), maxErrRunes))
	}
}

func (ev *evaluator) warnings() []string {
	out := slices.Clone(ev.warns)
	if more := ev.nWarn - MaxWarnings; more > 0 {
		out = append(out, fmt.Sprintf("%d more warnings", more))
	}
	return out
}

// sandbox opens the allowed libraries, removes what could print, load code or
// loop uninterruptibly, and installs rex and require.
func (ev *evaluator) sandbox(L *lua.LState) {
	for _, lib := range []struct {
		name string
		open lua.LGFunction
	}{
		{lua.BaseLibName, lua.OpenBase},
		{lua.TabLibName, lua.OpenTable},
		{lua.StringLibName, lua.OpenString},
		{lua.MathLibName, lua.OpenMath},
	} {
		L.Push(L.NewFunction(lib.open))
		L.Push(lua.LString(lib.name))
		L.Call(1, 0)
	}
	// pcall and xpcall are removed because a script could swallow the
	// timeout error in a loop; error() stays.
	for _, name := range []string{
		"dofile", "loadfile", "load", "loadstring", "print", "_printregs",
		"module", "getfenv", "setfenv", "pcall", "xpcall", "newproxy", "collectgarbage",
	} {
		L.SetGlobal(name, lua.LNil)
	}
	L.SetGlobal("require", L.NewFunction(ev.require))
	guardStrings(L)

	rex := L.NewTable()
	L.SetField(rex, "platform", lua.LString(platform()))
	L.SetField(rex, "set", L.NewFunction(ev.set))
	L.SetField(rex, "theme", L.NewFunction(ev.theme))
	L.SetGlobal("rex", rex)
}

// guardStrings caps the string built-ins that run in Go and cannot be
// interrupted: rep (output size) and the pattern functions (subject size).
func guardStrings(L *lua.LState) {
	tbl, ok := L.GetGlobal("string").(*lua.LTable)
	if !ok {
		panic("luacfg: string library missing")
	}
	wrap := func(name string, guard func(L *lua.LState, fname string)) {
		orig, ok := tbl.RawGetString(name).(*lua.LFunction)
		if !ok {
			panic("luacfg: string." + name + " missing")
		}
		tbl.RawSetString(name, L.NewFunction(func(L *lua.LState) int {
			guard(L, name)
			return orig.GFunction(L)
		}))
	}
	wrap("rep", func(L *lua.LState, fname string) {
		s, n := L.CheckString(1), L.CheckInt(2)
		if n > 0 && len(s) > 0 && (n > maxStrRep || len(s) > maxStrRep/n) {
			L.RaiseError("string.rep: result larger than %d KiB", maxStrRep>>10)
		}
	})
	pattern := func(L *lua.LState, fname string) {
		if len(L.CheckString(1)) > maxPatternSubject {
			L.RaiseError("string.%s: subject larger than %d KiB", fname, maxPatternSubject>>10)
		}
		if len(L.CheckString(2)) > maxPattern {
			L.RaiseError("string.%s: pattern larger than %d KiB", fname, maxPattern>>10)
		}
	}
	for _, name := range []string{"find", "match", "gmatch"} {
		wrap(name, pattern)
	}
	wrap("gsub", func(L *lua.LState, fname string) {
		pattern(L, fname)
		if r, ok := L.Get(3).(lua.LString); ok && len(r) > maxReplacement {
			L.RaiseError("string.gsub: replacement larger than %d KiB", maxReplacement>>10)
		}
	})
}

// where is the "config.lua:12" position of the Lua code that called the
// running Go function.
func where(L *lua.LState) string {
	return strings.TrimRight(strings.TrimSpace(L.Where(1)), ":")
}

// unsafeRune reports runes that must not reach a terminal: controls
// (including ESC), bidirectional controls, zero-width and other
// non-printing characters.
func unsafeRune(r rune) bool {
	switch {
	case r == utf8.RuneError, unicode.IsControl(r):
		return true
	case r == 0x200B, r == 0x200C, r == 0x200D, r == 0x2060, r == 0xFEFF:
		return true
	case r == 0x200E, r == 0x200F, r >= 0x202A && r <= 0x202E, r >= 0x2066 && r <= 0x2069:
		return true
	}
	return !unicode.IsPrint(r) && r != ' '
}

// checkString refuses a value string a terminal could misread.
func checkString(s string) error {
	if len(s) > MaxValue {
		return fmt.Errorf("longer than %d bytes", MaxValue)
	}
	if !utf8.ValidString(s) {
		return fmt.Errorf("not valid UTF-8")
	}
	for _, r := range s {
		if unsafeRune(r) {
			return fmt.Errorf("contains a control, escape, bidirectional or zero-width character (%U)", r)
		}
	}
	return nil
}

// set is rex.set(key, value). A refused call is a warning, not an error.
func (ev *evaluator) set(L *lua.LState) int {
	key := L.CheckString(1)
	val := L.Get(2)
	at := where(L)
	st, ok := settings.Find(key)
	switch {
	case key == "lua_config" || ok && (st.ReadOnly || slices.Contains(ev.opts.Deny, key)):
		ev.warn("%s: %q cannot be set from Lua (ignored)", at, key)
		return 0
	case !ok:
		ev.warn("%s: unknown setting %q (ignored)", at, key)
		return 0
	}
	raw, err := luaToSetting(st, val)
	if err == nil {
		raw, err = settings.Validate(key, raw)
	}
	if err != nil {
		ev.warn("%s: %q: %v (ignored)", at, key, err)
		return 0
	}
	ev.values[key] = raw
	ev.setAt[key] = at
	return 0
}

// luaToSetting converts a Lua value to the Go value for setting st. The
// Lua type must match the setting's type; only list settings take a table.
func luaToSetting(st settings.Setting, v lua.LValue) (any, error) {
	switch x := v.(type) {
	case lua.LBool:
		if st.Type != settings.TypeBool {
			return nil, fmt.Errorf("expects %s, got a bool", st.Type)
		}
		return bool(x), nil
	case lua.LNumber:
		f := float64(x)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, fmt.Errorf("not a finite number")
		}
		switch st.Type {
		case settings.TypeInt:
			if f != math.Trunc(f) || math.Abs(f) > 1<<40 {
				return nil, fmt.Errorf("%v is not a whole number", f)
			}
			return int(f), nil
		case settings.TypeFloat:
			return f, nil
		}
		return nil, fmt.Errorf("expects %s, got a number", st.Type)
	case lua.LString:
		if st.Type != settings.TypeString && st.Type != settings.TypeEnum {
			return nil, fmt.Errorf("expects %s, got a string", st.Type)
		}
		if err := checkString(string(x)); err != nil {
			return nil, err
		}
		return string(x), nil
	case *lua.LTable:
		if st.ListSep == "" {
			return nil, fmt.Errorf("expects %s, got a table", st.Type)
		}
		return joinList(st.ListSep, x)
	}
	return nil, fmt.Errorf("expects %s, got %s", st.Type, v.Type())
}

// joinList joins an array of strings with sep.
func joinList(sep string, t *lua.LTable) (string, error) {
	n := t.Len()
	if n > maxTableKeys {
		return "", fmt.Errorf("a list of more than %d entries", maxTableKeys)
	}
	count := 0
	t.ForEach(func(lua.LValue, lua.LValue) { count++ })
	if count != n {
		return "", fmt.Errorf("expects a plain list of strings")
	}
	parts := make([]string, 0, n)
	for i := 1; i <= n; i++ {
		s, ok := t.RawGetInt(i).(lua.LString)
		if !ok {
			return "", fmt.Errorf("list entry %d is not a string", i)
		}
		if err := checkString(string(s)); err != nil {
			return "", fmt.Errorf("list entry %d: %v", i, err)
		}
		if strings.Contains(string(s), sep) {
			return "", fmt.Errorf("list entry %d contains the separator %q", i, sep)
		}
		parts = append(parts, string(s))
	}
	return strings.Join(parts, sep), nil
}

// require is the only way to load code: a dotted module name maps to a file
// below the module directory, read through privfile.
func (ev *evaluator) require(L *lua.LState) int {
	name := L.CheckString(1)
	if !moduleName.MatchString(name) {
		L.RaiseError("require: invalid module name %q (use lower-case words joined by dots)", name)
	}
	if v, ok := ev.loaded[name]; ok {
		L.Push(v)
		return 1
	}
	if ev.loading[name] {
		L.RaiseError("require: loop while loading %q", name)
	}
	if len(ev.loaded) >= MaxModules {
		L.RaiseError("require: more than %d modules", MaxModules)
	}
	rel := strings.ReplaceAll(name, ".", "/") + ".lua"
	data, err := ev.readModule(rel)
	if err != nil {
		L.RaiseError("require %q: %s", name, flatten(err.Error(), maxErrRunes))
	}
	fn, err := L.Load(bytes.NewReader(data), "lua/"+rel)
	if err != nil {
		L.RaiseError("%s", luaMessage(err))
	}
	ev.loading[name] = true
	L.Push(fn)
	L.Push(lua.LString(name))
	L.Call(1, 1)
	ret := L.Get(-1)
	L.Pop(1)
	if ret == lua.LNil {
		ret = lua.LTrue
	}
	delete(ev.loading, name)
	ev.loaded[name] = ret
	L.Push(ret)
	return 1
}

// readModule reads <ModuleDir>/rel after checking that the module directory
// and every directory down to the file is private.
func (ev *evaluator) readModule(rel string) ([]byte, error) {
	dir := ev.opts.ModuleDir
	if dir == "" {
		return nil, fmt.Errorf("no module directory")
	}
	touch()
	cur := dir
	parts := strings.Split(filepath.ToSlash(filepath.Dir(rel)), "/")
	if parts[0] == "." {
		parts = nil
	}
	for i := -1; i < len(parts); i++ {
		if i >= 0 {
			cur = filepath.Join(cur, parts[i])
		}
		fi, err := os.Stat(cur)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, fmt.Errorf("module not found")
			}
			return nil, err
		}
		if !fi.IsDir() {
			return nil, fmt.Errorf("module not found")
		}
		if err := privfile.CheckDir(fi, cur); err != nil {
			return nil, fmt.Errorf("module directory: %w", err)
		}
	}
	data, err := privfile.Read(dir, rel, MaxScript)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("module not found")
		}
		return nil, err
	}
	if ev.total += len(data); ev.total > MaxTotal {
		return nil, fmt.Errorf("script and modules larger than %d KiB", MaxTotal>>10)
	}
	return data, nil
}
