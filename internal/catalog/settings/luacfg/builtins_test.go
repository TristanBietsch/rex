package luacfg

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
)

// luaOK runs a script whose own assert calls are the test.
func luaOK(t *testing.T, script string) {
	t.Helper()
	r := evalScript(t, script)
	require.NoError(t, r.Err)
}

// The string guards must keep gmatch working: its iterator is an upvalue of
// the library closure, which a plain wrapper loses.
func TestEvalGmatchIterates(t *testing.T) {
	luaOK(t, `
local n, got = 0, {}
for w in string.gmatch("one two three", "%a+") do n = n + 1 got[n] = w end
assert(n == 3 and got[1] == "one" and got[3] == "three", "gmatch")
local m = 0
for k, v in ("a=1, b=2"):gmatch("(%w+)=(%w+)") do m = m + 1 end
assert(m == 2, "method gmatch")
`)
}

// gfind is gopher-lua's alias of gmatch; it must get the same subject cap.
func TestEvalGfindIsGuarded(t *testing.T) {
	r := evalScript(t, "local s = string.rep('a', 1000000)\ns = s .. s .. s\nfor _ in string.gfind(s, 'a') do end")
	require.ErrorContains(t, r.Err, "subject larger")
	luaOK(t, `local n = 0 for _ in string.gfind("a b", "%a") do n = n + 1 end assert(n == 2)`)
}

// Built-ins that build a string in Go cannot be interrupted, so the
// heap watchdog cannot stop them: each must refuse a result over the cap
// before it allocates.
func TestEvalBuiltinResultCapped(t *testing.T) {
	big := `local s = string.rep("x", 1048576) local t = {} for i = 1, 300 do t[i] = s end `
	for name, script := range map[string]string{
		"table.concat":      big + `table.concat(t)`,
		"table.concat sep":  `local t = {} for i = 1, 3000 do t[i] = "" end table.concat(t, string.rep("y", 1000))`,
		"format many %s":    big + `string.format(string.rep("%s", 300), unpack(t))`,
		"gsub table repl":   `local s = string.rep("x", 1048576) string.gsub(string.rep("a", 300), "a", {a = s})`,
		"gsub %0 repl":      `string.gsub(string.rep("a", 250000), ".+", string.rep("%0", 500))`,
		"gsub function big": `local s = string.rep("x", 1048576) string.gsub(string.rep("a", 300), "a", function() return s end)`,
	} {
		start := time.Now()
		r := evalScript(t, script)
		require.ErrorContains(t, r.Err, "larger than", name)
		require.NotContains(t, r.Err.Error(), "memory", name)
		require.Less(t, time.Since(start), Timeout, name)
	}
}

// string.format takes Lua's directives only: Go's argument indexes and *
// widths would let one argument print many times, and widths stay at two
// digits as in Lua.
func TestEvalFormatDirectives(t *testing.T) {
	for _, f := range []string{"%999999d", "%.999f", "%[1]s%[1]s", "%*d"} {
		r := evalScript(t, `string.format("`+f+`", 1, 2)`)
		require.ErrorContains(t, r.Err, "invalid format", f)
	}
	luaOK(t, `
assert(string.format("%5.2f|%-3d|%s|%%|%x", 3.14159, 7, "hi", 255) == " 3.14|7  |hi|%|ff")
assert(string.format("%q", "a\"b") == '"a\\"b"')
assert(("%d-%d"):format(1, 2) == "1-2")
`)
}

// gsub is rebuilt in Go-linear time: a whole-subject replace must finish
// well inside the deadline (gopher-lua's own copies the buffer per match).
// A -race build runs several times slower, so it replaces a quarter as many
// matches; a quadratic gsub still blows the deadline at that size.
func TestEvalGsubLinear(t *testing.T) {
	n := 200000
	if raceOn {
		n = 50000
	}
	start := time.Now()
	luaOK(t, fmt.Sprintf(`
local r, n = string.gsub(string.rep("a", %[1]d), "a", "b")
assert(n == %[1]d and #r == %[1]d and r:sub(1, 3) == "bbb", "gsub")
`, n))
	require.Less(t, time.Since(start), Timeout)
}

// gsub keeps gopher-lua's semantics.
func TestEvalGsubSemantics(t *testing.T) {
	luaOK(t, `
local function eq(a, b, m) assert(a == b, m .. ": got " .. tostring(a)) end
eq((string.gsub("hello world", "o", "0")), "hell0 w0rld", "plain")
eq(select(2, string.gsub("hello world", "o", "0")), 2, "count")
eq((string.gsub("hello", "(l)(l)", "%2%1[%0]")), "hell[ll]o", "captures")
eq((string.gsub("abc", "%w", "%1%1")), "aabbcc", "%1 without captures is the match")
eq((string.gsub("abc", "b", "%%")), "a%c", "%%")
eq((string.gsub("abc", "b", "%x")), "a%xc", "%x kept")
eq((string.gsub("abc", "b", "x%")), "ax%c", "trailing %")
eq((string.gsub("abc", "x*", "-")), "-a-b-c-", "empty matches")
eq((string.gsub("abc", "()", "%1")), "1a2b3c4", "position capture")
eq((string.gsub("aaa", "a", "b", 2)), "bba", "limit")
eq((string.gsub("abc", "%w", {a = "1", c = false})), "1bc", "table")
eq((string.gsub("$x $y", "%$(%w+)", {x = "X", y = 2})), "X 2", "table capture key")
eq((string.gsub("abc", "%w", function(c) if c == "b" then return nil end return c:upper() end)), "AbC", "function")
eq((string.gsub("k=v", "(%w)=(%w)", function(a, b) return b .. "=" .. a end)), "v=k", "function captures")
eq((string.gsub("abc", "z", "y")), "abc", "no match")
local t = setmetatable({}, {__index = function(_, k) return k .. k end})
eq((string.gsub("ab", "%w", t)), "aabb", "__index")
`)
	r := evalScript(t, `string.gsub("abc", "b", "%2")`)
	require.ErrorContains(t, r.Err, "invalid capture index")
	r = evalScript(t, `string.gsub("abc", "b", {b = true})`)
	require.ErrorContains(t, r.Err, "invalid replacement value (a boolean)")
	r = evalScript(t, `string.gsub("abc", "b", 1)`)
	require.Error(t, r.Err)
}

func TestEvalBuiltinEdges(t *testing.T) {
	luaOK(t, `
assert((string.gsub("ab", "()", function(p) return p * 10 end)) == "10a20b30", "position capture to a function")
assert((string.gsub("ab", "()", {[2] = "X"})) == "aXb", "position capture as a table key")
assert(table.concat({"a", "b", "c"}, "-", 2) == "b-c", "concat range")
`)
	r := evalScript(t, `table.concat({"a", {}, "c"})`)
	require.ErrorContains(t, r.Err, "invalid value")
	r = evalScript(t, `string.format(string.rep("x", 200000))`)
	require.ErrorContains(t, r.Err, "string.format: result larger than")
	r = evalScript(t, `string.gsub("abc", "(", "x")`)
	require.Error(t, r.Err)
}

// gsub stops between matches once the deadline has passed.
func TestGsubCheckDeadline(t *testing.T) {
	L := lua.NewState(lua.Options{SkipOpenLibs: true})
	defer L.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	L.SetContext(ctx)
	g := gsubBuilder{L: L}
	L.Push(L.NewFunction(func(*lua.LState) int { cancel(); g.checkDeadline(); return 0 }))
	err := L.PCall(0, 0, nil)
	require.ErrorContains(t, err, "canceled")
}
