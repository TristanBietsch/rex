package luacfg

import (
	"fmt"
	"strings"

	lua "github.com/yuin/gopher-lua"
	"github.com/yuin/gopher-lua/pm"
)

// maxBuilt is the largest string a guarded built-in (string.rep,
// string.format, string.gsub, table.concat) may return. These run in Go and
// cannot be interrupted, so the heap watchdog cannot stop them: the bound is
// checked before, or while, the result is built.
const maxBuilt = 1 << 20

// raiseTooBig is the refusal of a built-in result over maxBuilt.
func raiseTooBig(L *lua.LState, fname string) {
	L.RaiseError("%s: result larger than %d KiB", fname, maxBuilt>>10)
}

// callOrig calls orig with this call's arguments as a real Lua call, so a
// library closure keeps its upvalues (gmatch's iterator is one), and returns
// what it returned.
func callOrig(L *lua.LState, orig *lua.LFunction) int {
	top := L.GetTop()
	L.Push(orig)
	for i := 1; i <= top; i++ {
		L.Push(L.Get(i))
	}
	L.Call(top, lua.MultRet)
	return L.GetTop() - top
}

// guardConcat refuses table.concat when the joined string would pass
// maxBuilt. It sums the lengths the way tableConcat walks the table and
// stops at the first value concat would refuse (concat raises then).
func guardConcat(L *lua.LState) {
	tbl := L.CheckTable(1)
	sep := L.OptString(2, "")
	n := tbl.Len()
	i, j := L.OptInt(3, 1), L.OptInt(4, n)
	i, j = max(min(i, n), 1), min(j, n)
	total := 0
	for k := i; k <= j; k++ {
		v := tbl.RawGetInt(k)
		if !lua.LVCanConvToString(v) {
			return
		}
		total += len(lua.LVAsString(v))
		if k != j {
			total += len(sep)
		}
		if total > maxBuilt {
			raiseTooBig(L, "table.concat")
		}
	}
}

// guardFormat refuses a string.format whose result could pass maxBuilt, and
// any directive outside Lua's grammar (%[flags][width][.precision]letter,
// width and precision at most two digits). gopher-lua hands the format to
// Go's fmt, where %[1]s and %*d would let one argument print many times
// and a width of 999999 pads a megabyte.
func guardFormat(L *lua.LState) {
	f := L.CheckString(1)
	dirs, q := 0, false
	for k := 0; k < len(f); k++ {
		if f[k] != '%' {
			continue
		}
		k++
		if k < len(f) && f[k] == '%' {
			continue
		}
		for k < len(f) && strings.IndexByte("-+ #0", f[k]) >= 0 {
			k++
		}
		k = skipDigits(L, f, k)
		if k < len(f) && f[k] == '.' {
			k = skipDigits(L, f, k+1)
		}
		if k >= len(f) || !isASCIILetter(f[k]) {
			L.RaiseError("string.format: invalid format %q (Lua directives only)", flatten(f, 40))
		}
		q = q || f[k] == 'q'
		dirs++
	}
	// Go writes "%!s(MISSING)" for a missing argument and "%!(EXTRA ...)"
	// for a spare one, so every byte of the format and every argument
	// counts; %q can quote a byte as four.
	per := 1
	if q {
		per = 4
	}
	total := 8*len(f) + dirs*200
	for a := 2; a <= L.GetTop(); a++ {
		if s, ok := L.Get(a).(lua.LString); ok {
			total += per*len(s) + 64
		} else {
			total += 512
		}
		if total > maxBuilt {
			raiseTooBig(L, "string.format")
		}
	}
	if total > maxBuilt {
		raiseTooBig(L, "string.format")
	}
}

// skipDigits steps over a width or precision, refusing more than two digits
// as Lua does.
func skipDigits(L *lua.LState, f string, k int) int {
	start := k
	for k < len(f) && f[k] >= '0' && f[k] <= '9' {
		k++
	}
	if k-start > 2 {
		L.RaiseError("string.format: invalid format (width or precision too long)")
	}
	return k
}

func isASCIILetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

// gsub is string.gsub rebuilt on gopher-lua's matcher. gopher-lua's own
// copies the whole result once per match (quadratic) and builds it in Go
// without a bound. This one writes the result once, refuses it past
// maxBuilt and stops at the deadline between matches. Replacement rules are
// gopher-lua's: %0-%9 captures (%1 is the match when there are none), %%
// a percent, any other %c kept as is; a table or function value of nil or
// false keeps the match.
func gsub(L *lua.LState) int {
	str := L.CheckString(1)
	pat := L.CheckString(2)
	checkPattern(L, "gsub", str, pat)
	L.CheckTypes(3, lua.LTString, lua.LTTable, lua.LTFunction)
	repl := L.CheckAny(3)
	if r, ok := repl.(lua.LString); ok && len(r) > maxReplacement {
		L.RaiseError("string.gsub: replacement larger than %d KiB", maxReplacement>>10)
	}
	limit := L.OptInt(4, -1)
	mds, err := pm.Find(pat, []byte(str), 0, limit)
	if err != nil {
		L.RaiseError("%s", err.Error())
	}
	g := gsubBuilder{L: L, str: str}
	last := 0
	for n, m := range mds {
		if n%1024 == 0 {
			g.checkDeadline()
		}
		start, end := m.Capture(0), m.Capture(1)
		g.write(str[last:start])
		last = end
		switch r := repl.(type) {
		case lua.LString:
			g.expand(string(r), m)
			continue
		case *lua.LTable:
			key := g.capture(m, 0, true)
			if m.CaptureLength() > 2 {
				key = g.capture(m, 2, true)
			}
			g.value(L.GetTable(r, key), str[start:end])
		case *lua.LFunction:
			L.Push(r)
			nargs := 1
			if m.CaptureLength() > 2 {
				nargs = 0
				for i := 2; i < m.CaptureLength(); i += 2 {
					L.Push(g.capture(m, i, true))
					nargs++
				}
			} else {
				L.Push(g.capture(m, 0, true))
			}
			L.Call(nargs, 1)
			g.value(L.Get(-1), str[start:end])
			L.Pop(1)
		}
	}
	g.write(str[last:])
	L.Push(lua.LString(g.b.String()))
	L.Push(lua.LNumber(len(mds)))
	return 2
}

// gsubBuilder is gsub's result under construction.
type gsubBuilder struct {
	L   *lua.LState
	str string
	b   strings.Builder
}

func (g *gsubBuilder) write(s string) {
	if g.b.Len()+len(s) > maxBuilt {
		raiseTooBig(g.L, "string.gsub")
	}
	g.b.WriteString(s)
}

func (g *gsubBuilder) checkDeadline() {
	if ctx := g.L.Context(); ctx != nil && ctx.Err() != nil {
		g.L.RaiseError("%s", ctx.Err().Error())
	}
}

// value writes a table or function replacement: nil or false keeps the
// match, a string or number replaces it, anything else is an error (as in
// Lua; gopher-lua would write nothing).
func (g *gsubBuilder) value(v lua.LValue, match string) {
	switch {
	case lua.LVIsFalse(v):
		g.write(match)
	case lua.LVCanConvToString(v):
		g.write(lua.LVAsString(v))
	default:
		g.L.RaiseError("invalid replacement value (a %s)", v.Type().String())
	}
}

// capture returns capture idx (0 the whole match, 2 the first capture, ...)
// as gopher-lua's capturedString does: a position capture is a number
// (asValue) or its decimal text.
func (g *gsubBuilder) capture(m *pm.MatchData, idx int, asValue bool) lua.LValue {
	if idx > 2 && idx >= m.CaptureLength() {
		g.L.RaiseError("invalid capture index")
	}
	if idx == 2 && idx >= m.CaptureLength() {
		idx = 0
	}
	if m.IsPosCapture(idx) {
		if asValue {
			return lua.LNumber(m.Capture(idx))
		}
		return lua.LString(fmt.Sprint(m.Capture(idx)))
	}
	return lua.LString(g.str[m.Capture(idx):m.Capture(idx+1)])
}

// expand writes the replacement string r for match m.
func (g *gsubBuilder) expand(r string, m *pm.MatchData) {
	for i := 0; i < len(r); i++ {
		c := r[i]
		if c != '%' || i == len(r)-1 {
			g.write(r[i : i+1])
			continue
		}
		i++
		switch d := r[i]; {
		case d == '%':
			g.write("%")
		case d >= '0' && d <= '9':
			g.write(lua.LVAsString(g.capture(m, 2*int(d-'0'), false)))
		default:
			g.write(r[i-1 : i+1])
		}
	}
}
