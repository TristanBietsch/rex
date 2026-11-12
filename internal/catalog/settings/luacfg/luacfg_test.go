package luacfg

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/testutil/testperm"
)

// fixture is a private config directory with a config.lua and lua/ below it.
type fixture struct {
	t    *testing.T
	dir  string
	opts Opts
}

func newFixture(t *testing.T, script string) *fixture {
	t.Helper()
	ResetMemo()
	dir := filepath.Join(t.TempDir(), "rex")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "lua"), 0o700))
	f := &fixture{t: t, dir: dir, opts: Opts{File: filepath.Join(dir, "config.lua"), ModuleDir: filepath.Join(dir, "lua")}}
	f.write("config.lua", script)
	return f
}

func (f *fixture) write(rel, body string) {
	f.t.Helper()
	p := filepath.Join(f.dir, filepath.FromSlash(rel))
	require.NoError(f.t, os.MkdirAll(filepath.Dir(p), 0o700))
	require.NoError(f.t, os.WriteFile(p, []byte(body), 0o600))
}

func (f *fixture) eval() Result { return Eval(context.Background(), f.opts) }

func evalScript(t *testing.T, script string) Result { return newFixture(t, script).eval() }

func TestEvalSet(t *testing.T) {
	r := evalScript(t, `
rex.set("theme", "noir")
rex.set("reduce_motion", true)
rex.set("auto_trust_dirs", { "~/dev", "~/work" })
rex.set("board_hidden_sections", { "done", "waiting" })
`)
	require.NoError(t, r.Err)
	require.True(t, r.Exists)
	require.Equal(t, "noir", r.Values["theme"])
	require.Equal(t, true, r.Values["reduce_motion"])
	require.Equal(t, "~/dev"+string(os.PathListSeparator)+"~/work", r.Values["auto_trust_dirs"])
	require.Equal(t, "done,waiting", r.Values["board_hidden_sections"])
	require.Equal(t, "config.lua:2", r.SetAt["theme"])
	require.Equal(t, "config.lua:3", r.SetAt["reduce_motion"])
	require.Empty(t, r.Warnings)
}

func TestEvalSet_LaterCallWinsAndMovesPosition(t *testing.T) {
	r := evalScript(t, "rex.set('theme','noir')\nrex.set('theme','paper')")
	require.Equal(t, "paper", r.Values["theme"])
	require.Equal(t, "config.lua:2", r.SetAt["theme"])
}

func TestEvalSet_NumbersAndTypes(t *testing.T) {
	var intID, floatID string
	for _, s := range settings.Registry {
		switch {
		case s.Type == settings.TypeInt && intID == "" && !s.ReadOnly:
			intID = s.ID
		case s.Type == settings.TypeFloat && floatID == "" && !s.ReadOnly:
			floatID = s.ID
		}
	}
	require.NotEmpty(t, intID)
	st, _ := settings.Find(intID)
	in := int(st.Min)
	r := evalScript(t, "rex.set('"+intID+"', "+itoa(in)+")\nrex.set('"+intID+"', 1.5)\nrex.set('reduce_motion', 'yes')\nrex.set('theme', true)\nrex.set('theme', 3)\nrex.set('"+intID+"', 0/0)\nrex.set('"+intID+"', 2^50)\nrex.set('reduce_motion', {})\nrex.set('theme', {})\nrex.set('theme', print)")
	require.NoError(t, r.Err)
	require.Equal(t, in, r.Values[intID])
	require.Len(t, r.Warnings, 9)
	require.Contains(t, r.Warnings[0], "not a whole number")
	require.Contains(t, r.Warnings[1], "expects bool, got a string")
	require.Contains(t, r.Warnings[2], "expects string, got a bool")
	require.Contains(t, r.Warnings[3], "expects string, got a number")
	require.Contains(t, r.Warnings[4], "not a finite number")
	require.Contains(t, r.Warnings[5], "not a whole number")
	require.Contains(t, r.Warnings[6], "expects bool, got a table")
	require.Contains(t, r.Warnings[7], "expects string, got a table")
	require.Contains(t, r.Warnings[8], "expects string, got nil")
	if floatID != "" {
		fst, _ := settings.Find(floatID)
		r = evalScript(t, "rex.set('"+floatID+"', "+ftoa(fst.Min)+")")
		require.Equal(t, fst.Min, r.Values[floatID])
	}
}

func TestEvalSet_Refusals(t *testing.T) {
	f := newFixture(t, `
rex.set("nope", 1)
rex.set("lua_config", false)
rex.set("lua_config_path", "/tmp/evil.lua")
rex.set("skip_permissions", true)
rex.set("theme", "bad name!")
`)
	f.opts.Deny = []string{"skip_permissions"}
	r := f.eval()
	require.NoError(t, r.Err)
	require.Empty(t, r.Values)
	require.Len(t, r.Warnings, 5)
	require.Contains(t, r.Warnings[0], `config.lua:2: unknown setting "nope"`)
	require.Contains(t, r.Warnings[1], `"lua_config" cannot be set from Lua`)
	require.Contains(t, r.Warnings[2], `"lua_config_path" cannot be set from Lua`)
	require.Contains(t, r.Warnings[3], `"skip_permissions" cannot be set from Lua`)
	require.Contains(t, r.Warnings[4], `"theme"`)
}

func TestEvalNormalize_SameMessageAsValidate(t *testing.T) {
	_, want := settings.Validate("auto_trust_dirs", "relative/dir")
	require.Error(t, want)
	r := evalScript(t, `rex.set("auto_trust_dirs", "relative/dir")`)
	require.Contains(t, r.Warnings[0], want.Error())
	require.Empty(t, r.Values)
}

func TestEvalSet_ListRules(t *testing.T) {
	sep := string(os.PathListSeparator)
	r := evalScript(t, `
rex.set("auto_trust_dirs", { "~/a", 3 })
rex.set("auto_trust_dirs", { "~/a`+sep+`b" })
rex.set("auto_trust_dirs", { x = "~/a" })
rex.set("auto_trust_dirs", { "~/a\27" })
`+"local big = {} for i = 1, 100 do big[i] = '~/x' end rex.set('auto_trust_dirs', big)")
	require.Empty(t, r.Values)
	require.Len(t, r.Warnings, 5)
	require.Contains(t, r.Warnings[0], "not a string")
	require.Contains(t, r.Warnings[1], "contains the separator")
	require.Contains(t, r.Warnings[2], "plain list")
	require.Contains(t, r.Warnings[3], "control")
	require.Contains(t, r.Warnings[4], "more than 64")
}

func TestEvalPlatform(t *testing.T) {
	r := evalScript(t, `if rex.platform == "`+platform()+`" then rex.set("reduce_motion", true) end`)
	require.Equal(t, true, r.Values["reduce_motion"])
}

func TestEval_MissingFileIsEmpty(t *testing.T) {
	ResetMemo()
	r := Eval(context.Background(), Opts{File: filepath.Join(t.TempDir(), "nope", "config.lua")})
	require.False(t, r.Exists)
	require.NoError(t, r.Err)
	dir := t.TempDir()
	r = Eval(context.Background(), Opts{File: filepath.Join(dir, "config.lua")})
	require.False(t, r.Exists)
	require.NoError(t, r.Err)
}

func TestEval_SyntaxAndRuntimeErrors(t *testing.T) {
	r := evalScript(t, "rex.set('theme', 'noir'")
	require.ErrorContains(t, r.Err, "config.lua")
	require.Empty(t, r.Values)
	r = evalScript(t, "rex.set('theme', 'noir')\nerror('boom')")
	require.ErrorContains(t, r.Err, "config.lua:2: boom")
	require.Empty(t, r.Values, "all or nothing")
	r = evalScript(t, "local x = nil + 1")
	require.ErrorContains(t, r.Err, "cannot perform")
	r = evalScript(t, "rex.set({})")
	require.Error(t, r.Err)
}

func TestEvalNoDangerousGlobals(t *testing.T) {
	for _, g := range []string{"os", "io", "package", "debug", "coroutine", "dofile", "loadfile", "load", "loadstring", "print", "_printregs", "module", "getfenv", "setfenv", "pcall", "xpcall", "newproxy", "collectgarbage", "channel"} {
		r := evalScript(t, "if "+g+" ~= nil then error('"+g+" is reachable') end")
		require.NoError(t, r.Err, g)
	}
	r := evalScript(t, "print('hi')")
	require.ErrorContains(t, r.Err, "attempt to call")
}

func TestEvalPrintNeverReachesStdout(t *testing.T) {
	rd, wr, err := os.Pipe()
	require.NoError(t, err)
	old := os.Stdout
	os.Stdout = wr
	r := evalScript(t, "print('leak') io.write('leak') _printregs()")
	os.Stdout = old
	require.NoError(t, wr.Close())
	out, _ := readAllString(rd)
	require.Error(t, r.Err)
	require.Empty(t, out)
}

func TestEvalLibsStillWork(t *testing.T) {
	r := evalScript(t, `
local t = {}
for i = 1, 3 do table.insert(t, math.floor(i * 1.5)) end
rex.set("theme", string.upper("noir"):lower() .. (#t == 3 and "" or "x"))
`)
	require.NoError(t, r.Err)
	require.Equal(t, "noir", r.Values["theme"])
}

func TestEvalRequire(t *testing.T) {
	f := newFixture(t, `
local ui = require("ui")
require("ui")
ui.apply()
require("sub.deep")
`)
	f.write("lua/ui.lua", "local M = {}\nfunction M.apply()\n  rex.set('theme', 'paper')\nend\nreturn M\n")
	f.write("lua/sub/deep.lua", "rex.set('reduce_motion', true)")
	r := f.eval()
	require.NoError(t, r.Err)
	require.Equal(t, "paper", r.Values["theme"])
	require.Equal(t, "lua/ui.lua:3", r.SetAt["theme"])
	require.Equal(t, "lua/sub/deep.lua:1", r.SetAt["reduce_motion"])
}

func TestEvalRequireRefusals(t *testing.T) {
	for _, name := range []string{"../x", "a/b", "/etc/x", "A", "a..b", "a.", "", "a b", `a\b`} {
		r := evalScript(t, "require("+quoteLua(name)+")")
		require.ErrorContains(t, r.Err, "invalid module name", name)
	}
	r := evalScript(t, `require("missing")`)
	require.ErrorContains(t, r.Err, "module not found")
	r = evalScript(t, `require("a.b")`)
	require.ErrorContains(t, r.Err, "module not found")
}

func TestEvalRequireIgnoresLuaPath(t *testing.T) {
	other := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(other, "evil.lua"), []byte("rex.set('theme','evil')"), 0o600))
	t.Setenv("LUA_PATH", other+"/?.lua")
	t.Setenv("LUA_CPATH", other+"/?.so")
	r := evalScript(t, `require("evil")`)
	require.ErrorContains(t, r.Err, "module not found")
}

func TestEvalRequireCycleAndLimits(t *testing.T) {
	f := newFixture(t, `require("a")`)
	f.write("lua/a.lua", `require("b")`)
	f.write("lua/b.lua", `require("a")`)
	require.ErrorContains(t, f.eval().Err, "loop while loading")

	f = newFixture(t, "for i = 1, 70 do require('m' .. i) end")
	for i := 1; i <= 70; i++ {
		f.write("lua/m"+itoa(i)+".lua", "return "+itoa(i))
	}
	require.ErrorContains(t, f.eval().Err, "more than 64 modules")

	f = newFixture(t, `require("big1") require("big2") require("big3") require("big4") require("big5")`)
	for i := 1; i <= 5; i++ {
		f.write("lua/big"+itoa(i)+".lua", "-- "+strings.Repeat("x", 250<<10)+"\n")
	}
	require.ErrorContains(t, f.eval().Err, "larger than 1024 KiB")

	f = newFixture(t, `require("huge")`)
	f.write("lua/huge.lua", "-- "+strings.Repeat("x", MaxScript)+"\n")
	require.ErrorContains(t, f.eval().Err, "larger than 256 KiB")

	f = newFixture(t, `require("syn")`)
	f.write("lua/syn.lua", "x = = 1")
	require.ErrorContains(t, f.eval().Err, "lua/syn.lua")
}

func TestEvalRequireWithoutModuleDir(t *testing.T) {
	f := newFixture(t, `require("ui")`)
	f.opts.ModuleDir = ""
	require.ErrorContains(t, f.eval().Err, "no module directory")
}

func TestEvalRequireNonDirModule(t *testing.T) {
	f := newFixture(t, `require("a.b")`)
	f.write("lua/a", "file not dir")
	require.ErrorContains(t, f.eval().Err, "module not found")
}

func TestEval_ScriptTooBig(t *testing.T) {
	r := evalScript(t, "-- "+strings.Repeat("x", MaxScript)+"\n")
	require.ErrorContains(t, r.Err, "larger than 256 KiB")
}

// CPU

func TestEvalTimeout(t *testing.T) {
	start := time.Now()
	r := evalScript(t, "while true do end")
	require.Less(t, time.Since(start), 300*time.Millisecond+50*time.Millisecond)
	require.ErrorContains(t, r.Err, "timed out")
	require.Empty(t, r.Values)
}

func TestEvalDeepRecursionHitsStackLimit(t *testing.T) {
	r := evalScript(t, "local function f(n) return 1 + f(n + 1) end f(1)")
	require.Error(t, r.Err)
	require.NotContains(t, r.Err.Error(), "timed out")
}

func TestEvalPathologicalPatternReturnsAtDeadline(t *testing.T) {
	start := time.Now()
	r := evalScript(t, `
local s = string.rep("a", 200000)
string.find(s, ".-.-.-.-.-.-b")
`)
	require.Less(t, time.Since(start), Timeout+150*time.Millisecond)
	require.Error(t, r.Err)
}

func TestEvalPcallCannotSwallowTheDeadline(t *testing.T) {
	start := time.Now()
	r := evalScript(t, "while true do local ok = pcall and pcall(function() while true do end end) end")
	require.Less(t, time.Since(start), 350*time.Millisecond)
	require.Error(t, r.Err)
}

// Memory

func TestEvalStringRepCapped(t *testing.T) {
	r := evalScript(t, `local s = string.rep("x", 1e9)`)
	require.ErrorContains(t, r.Err, "string.rep: result larger than 1024 KiB")
	r = evalScript(t, `local s = ("ab"):rep(2e6)`)
	require.ErrorContains(t, r.Err, "string.rep")
	r = evalScript(t, `rex.set("theme", ("x"):rep(0) .. "noir" .. ("y"):rep(-1))`)
	require.Equal(t, "noir", r.Values["theme"])
}

func TestEvalPatternSubjectCapped(t *testing.T) {
	for _, call := range []string{"find", "match", "gmatch", "gsub"} {
		r := evalScript(t, "local s = string.rep('a', 1000000)\ns = s .. s .. s\nstring."+call+"(s, 'a', 'b')")
		require.Error(t, r.Err, call)
	}
	r := evalScript(t, "local s = ('a'):rep(1000)\nstring.find(s, ('a'):rep(2000))")
	require.ErrorContains(t, r.Err, "pattern larger")
	r = evalScript(t, "string.gsub('abc', 'b', ('x'):rep(2000))")
	require.ErrorContains(t, r.Err, "replacement larger")
	r = evalScript(t, "rex.set('theme', (string.gsub('xoir', 'x', 'n')))")
	require.Equal(t, "noir", r.Values["theme"])
}

func TestEvalDoublingStringStops(t *testing.T) {
	r := evalScript(t, "local s = 'xxxxxxxxxxxxxxxx' while true do s = s .. s end")
	require.Error(t, r.Err)
	require.Regexp(t, "memory|timed out", r.Err.Error())
}

func TestEvalHugeTableStops(t *testing.T) {
	r := evalScript(t, "local t = {} for i = 1, 1e8 do t[i] = {i} end")
	require.Error(t, r.Err)
	require.Regexp(t, "memory|timed out", r.Err.Error())
}

// Escapes

func TestEvalRefusesEscapeBytesInValues(t *testing.T) {
	r := evalScript(t, `
rex.set("summary_model", "\27]52;c;Zm9v\7")
rex.set("summary_model", "a`+"\u202e"+`b")
rex.set("summary_model", "a`+"\u200b"+`b")
rex.set("summary_model", "a\nb")
rex.set("summary_model", "\255\254")
rex.set("summary_model", string.rep("x", 5000))
`)
	require.NoError(t, r.Err)
	require.Empty(t, r.Values)
	require.Len(t, r.Warnings, 6)
	for _, w := range r.Warnings {
		require.NotContains(t, w, "\x1b")
		require.NotContains(t, w, "\u202e")
	}
}

func TestEvalErrorTextIsFlattened(t *testing.T) {
	r := evalScript(t, `error("\27[2J\27]0;pwned\7evil\ndetail")`)
	require.Error(t, r.Err)
	require.NotContains(t, r.Err.Error(), "\x1b")
	require.NotContains(t, r.Err.Error(), "\x07")
	require.NotContains(t, r.Err.Error(), "detail")
	r = evalScript(t, `error("`+strings.Repeat("x", 1000)+`")`)
	require.LessOrEqual(t, len([]rune(r.Err.Error())), 260)
}

func TestEvalWarningsCapped(t *testing.T) {
	r := evalScript(t, "for i = 1, 100 do rex.set('nope' .. i, 1) end")
	require.NoError(t, r.Err)
	require.Len(t, r.Warnings, MaxWarnings+1)
	require.Equal(t, "80 more warnings", r.Warnings[MaxWarnings])
}

func TestEvalWarningTextEscapesKeys(t *testing.T) {
	r := evalScript(t, `rex.set("\27[31mred", 1)`)
	require.Len(t, r.Warnings, 1)
	require.NotContains(t, r.Warnings[0], "\x1b")
}

// Ownership and files

func TestEval_RefusesLooseFiles(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("windows modes carry no group/other bits")
	}
	for _, mode := range []os.FileMode{0o666, 0o620} {
		f := newFixture(t, "rex.set('theme','noir')")
		testperm.Loosen(t, f.opts.File, mode)
		r := f.eval()
		require.True(t, r.Exists)
		require.ErrorContains(t, r.Err, "writable by group or others")
		require.Empty(t, r.Values)
	}
}

func TestEval_RefusesLooseDirectory(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("windows modes carry no group/other bits")
	}
	f := newFixture(t, "rex.set('theme','noir')")
	testperm.Loosen(t, f.dir, 0o777)
	r := f.eval()
	require.ErrorContains(t, r.Err, "config directory")
}

func TestEval_RefusesLooseModule(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("windows modes carry no group/other bits")
	}
	f := newFixture(t, `require("ui")`)
	f.write("lua/ui.lua", "x = 1")
	testperm.Loosen(t, filepath.Join(f.dir, "lua", "ui.lua"), 0o666)
	require.ErrorContains(t, f.eval().Err, "writable by group or others")
	f.write("lua/ui.lua", "x = 1")
	testperm.Loosen(t, filepath.Join(f.dir, "lua"), 0o777)
	require.ErrorContains(t, f.eval().Err, "module directory")
}

func TestEval_ConfigIsADirectory(t *testing.T) {
	f := newFixture(t, "")
	require.NoError(t, os.Remove(f.opts.File))
	require.NoError(t, os.Mkdir(f.opts.File, 0o700))
	r := f.eval()
	require.True(t, r.Exists)
	require.ErrorContains(t, r.Err, "not a regular file")
}

func TestEval_StatErrorOnDirectory(t *testing.T) {
	skipNoSymlinks(t)
	base := t.TempDir()
	loop := filepath.Join(base, "loop")
	require.NoError(t, os.Symlink("loop", loop))
	r := Eval(context.Background(), Opts{File: filepath.Join(loop, "config.lua")})
	require.True(t, r.Exists)
	require.Error(t, r.Err)
}

func TestEval_ParentContextCancel(t *testing.T) {
	f := newFixture(t, "while true do end")
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	r := Eval(ctx, f.opts)
	require.ErrorContains(t, r.Err, "stopped")
}

func TestEvalInternalPanicIsAnError(t *testing.T) {
	r := evalScript(t, "rex.set(nil, 1)")
	require.Error(t, r.Err)
}

func FuzzEval(f *testing.F) {
	for _, s := range []string{
		"rex.set('theme','noir')", "while false do end", "rex.set()", "local t={} t[1]=t",
		"rex.set('auto_trust_dirs', {'~/a'})", "require('x')", "string.rep('x', -1)", "\xff\xfe",
	} {
		f.Add(s)
	}
	dir := f.TempDir()
	cfg := filepath.Join(dir, "config.lua")
	f.Fuzz(func(t *testing.T, script string) {
		if len(script) > 4096 {
			return
		}
		if err := os.WriteFile(cfg, []byte(script), 0o600); err != nil {
			t.Fatal(err)
		}
		r := Eval(context.Background(), Opts{File: cfg, ModuleDir: filepath.Join(dir, "lua")})
		if r.Err != nil {
			require.Empty(t, r.Values)
		}
		for _, v := range r.Values {
			if s, ok := v.(string); ok {
				require.NoError(t, checkString(s))
			}
		}
	})
}

func BenchmarkEval50Lines(b *testing.B) {
	ResetMemo()
	dir := b.TempDir()
	var sb strings.Builder
	for i := 0; i < 25; i++ {
		sb.WriteString("rex.set('theme', 'noir')\nrex.set('reduce_motion', true)\n")
	}
	cfg := filepath.Join(dir, "config.lua")
	require.NoError(b, os.WriteFile(cfg, []byte(sb.String()), 0o600))
	o := Opts{File: cfg, ModuleDir: filepath.Join(dir, "lua")}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if r := Eval(context.Background(), o); r.Err != nil {
			b.Fatal(r.Err)
		}
	}
}

// Windows paths in a Lua list: a drive letter's colon is not the Windows
// list separator (;), so the entries join as rex config set would take them.
func TestEvalSet_WindowsPathList(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the list separator is ';' only on Windows")
	}
	r := evalScript(t, `rex.set("auto_trust_dirs", { "C:\\dev", "D:\\work" })`)
	require.NoError(t, r.Err)
	require.Empty(t, r.Warnings)
	require.Equal(t, `C:\dev;D:\work`, r.Values["auto_trust_dirs"])
}
