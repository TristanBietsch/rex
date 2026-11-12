package setup

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/catalog/settings/luacfg"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

// luaConfigHome points HOME at a fresh temp dir, writes ~/.config/rex/config.lua (and config.yaml when yaml is
// not empty) under the test's HOME and installs the Lua layer for the test.
func luaConfigHome(t *testing.T, lua, yaml string) {
	t.Helper()
	luacfg.ResetMemo()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	dir, err := paths.ConfigDir()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "lua"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.lua"), []byte(lua), 0o600))
	if yaml != "" {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(yaml), 0o600))
	}
	settings.SetLayer(luacfg.Layer())
	t.Cleanup(func() { settings.SetLayer(nil); luacfg.ResetMemo() })
}

func TestConfigSetAndReset_LuaKeyExitsFiveWithLocation(t *testing.T) {
	luaConfigHome(t, "rex.set('spinner', 'moon')\nrex.set('theme', 'noir')", "")
	for name, run := range map[string]func() error{
		"set":   func() error { return configSet([]string{"theme", "paper"}) },
		"reset": func() error { return configReset([]string{"theme"}) },
	} {
		err := run()
		var ec core.ExitCoder
		require.ErrorAs(t, err, &ec, name)
		require.Equal(t, core.ExitInvalidArgs, ec.ExitCode(), name)
		require.Contains(t, err.Error(), "config.lua:2", name)
	}
	// config.yaml is untouched; another key still saves.
	require.NoError(t, configSet([]string{"row_density", "compact"}))
	cfg, err := paths.ConfigFile()
	require.NoError(t, err)
	raw, err := os.ReadFile(cfg)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "theme")
	require.NotContains(t, string(raw), "spinner")
}

func TestConfigSet_LuaConfigFlagItselfStaysEditable(t *testing.T) {
	luaConfigHome(t, "rex.set('theme', 'noir')", "")
	require.NoError(t, configSet([]string{"lua_config", "false"}))
	s, _, err := settings.LoadDefault()
	require.NoError(t, err)
	require.Equal(t, "default", s.Get("theme"), "off: config.lua is ignored and rows unlock")
	require.NoError(t, configSet([]string{"theme", "paper"}))
}

// runFmt runs fn with a Printer in format and returns stdout and stderr.
func runFmt(t *testing.T, format string, fn func() error) (stdout, stderr string, err error) {
	t.Helper()
	var out, errw bytes.Buffer
	env := core.Env{Getenv: func(string) string { return "" }, Width: 100, Scheme: "default", GOOS: "linux"}
	p, perr := core.NewPrinter(core.FormatFlags{Output: format}, env, &out, &errw)
	require.NoError(t, perr)
	core.SetOut(p)
	t.Cleanup(func() { core.SetOut(nil) })
	err = fn()
	return out.String(), errw.String(), err
}

func TestConfigList_SourceAndSetAt(t *testing.T) {
	luaConfigHome(t, "rex.set('spinner', 'moon')\nrex.set('theme', 'noir')", "row_density: compact\n")

	js, _, err := runFmt(t, "json", func() error { return configList(nil) })
	require.NoError(t, err)
	require.Contains(t, js, `{"id":"theme","section":"Appearance","value":"noir","default":"default","source":"lua","set_at":"config.lua:2"}`)
	require.Contains(t, js, `"id":"row_density","section":"Appearance","value":"compact","default":"normal","source":"file"}`)
	require.Contains(t, js, `"id":"reduce_motion","section":"Appearance","value":false,"default":false,"source":"default"}`)

	ag, _, err := runFmt(t, "agent", func() error { return configList(nil) })
	require.NoError(t, err)
	require.Contains(t, ag, "setting id=theme section=Appearance value=noir default=default source=lua set_at=config.lua:2\n")
	require.Contains(t, ag, "setting id=row_density section=Appearance value=compact default=normal source=file set_at=\"\"\n")

	txt, _, err := runFmt(t, "text", func() error { return configList(nil) })
	require.NoError(t, err)
	require.Contains(t, txt, "SOURCE")
	require.Regexp(t, `theme\s+noir\s+lua config\.lua:2`, txt)
	require.Regexp(t, `row_density\s+compact\s+config\.yaml`, txt)

	plain, _, err := runFmt(t, "txt", func() error { return configList(nil) })
	require.NoError(t, err)
	require.Contains(t, plain, "  theme                      noir\n", "txt keeps its layout")
	require.NotContains(t, plain, "config.lua")
}

func TestConfigList_LegacyJSONKeepsItsShape(t *testing.T) {
	luaConfigHome(t, "rex.set('theme', 'noir')", "")
	out, _, err := runFmt(t, "", func() error { return configList([]string{"-json"}) })
	require.NoError(t, err)
	require.Contains(t, out, `{"id":"theme","section":"Appearance","value":"noir","default":"default"}`+"\n")
}

func TestConfigGet_SourceAndSetAt(t *testing.T) {
	luaConfigHome(t, "rex.set('theme', 'noir')", "")
	js, _, err := runFmt(t, "json", func() error { return configGet([]string{"theme"}) })
	require.NoError(t, err)
	require.Contains(t, js, `"data":{"id":"theme","value":"noir","source":"lua","set_at":"config.lua:1"}`)
	ag, _, err := runFmt(t, "agent", func() error { return configGet([]string{"theme"}) })
	require.NoError(t, err)
	require.Contains(t, ag, "source=lua set_at=config.lua:1")
	txt, _, err := runFmt(t, "text", func() error { return configGet([]string{"theme"}) })
	require.NoError(t, err)
	require.Contains(t, txt, "noir")
	require.Contains(t, txt, "lua config.lua:1")
	plain, _, err := runFmt(t, "txt", func() error { return configGet([]string{"theme"}) })
	require.NoError(t, err)
	require.Equal(t, "noir\n", plain, "txt is the bare value")
	js, _, err = runFmt(t, "json", func() error { return configGet([]string{"spinner"}) })
	require.NoError(t, err)
	require.Contains(t, js, `"data":{"id":"spinner","value":"braille","source":"default"}`)
}

func TestConfig_ReportsABrokenConfigLuaOnStderrOnly(t *testing.T) {
	luaConfigHome(t, "error('\\27[2Jboom')", "")
	out, errOut, err := runFmt(t, "text", func() error { return configGet([]string{"theme"}) })
	require.NoError(t, err)
	require.Equal(t, "default\n", out)
	require.Contains(t, errOut, "config.lua")
	require.NotContains(t, errOut, "\x1b[2J")
}

func TestConfigEditLua_CreatesTemplatePrivately(t *testing.T) {
	luaConfigHome(t, "", "")
	dir, err := paths.ConfigDir()
	require.NoError(t, err)
	file := filepath.Join(dir, "config.lua")
	require.NoError(t, os.Remove(file))
	require.NoError(t, os.Remove(filepath.Join(dir, "lua")))
	require.NoError(t, os.Remove(dir))
	t.Setenv("EDITOR", "true")
	if runtime.GOOS == "windows" {
		t.Skip("no `true` editor on Windows")
	}
	_, _, err = runFmt(t, "text", func() error { return configEdit([]string{"-lua"}) })
	require.NoError(t, err)
	body, err := os.ReadFile(file)
	require.NoError(t, err)
	require.Contains(t, string(body), "rex.set(")
	fi, err := os.Stat(file)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), fi.Mode().Perm())
	di, err := os.Stat(dir)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o700), di.Mode().Perm())

	// An existing file is never overwritten.
	require.NoError(t, os.WriteFile(file, []byte("-- mine\n"), 0o600))
	_, _, err = runFmt(t, "text", func() error { return configEdit([]string{"-lua"}) })
	require.NoError(t, err)
	body, _ = os.ReadFile(file)
	require.Equal(t, "-- mine\n", string(body))
}

func TestConfigLuaTemplateEvaluatesCleanly(t *testing.T) {
	luaConfigHome(t, luaTemplate, "")
	s, rep, _, err := settings.LoadDefaultReport()
	require.NoError(t, err)
	require.NoError(t, rep.Err)
	require.Empty(t, rep.Warnings)
	require.NotNil(t, s)
}

func TestConfigLuaExport(t *testing.T) {
	luaConfigHome(t, "rex.set('theme', 'noir')", "spinner: moon\nreduce_motion: true\nmax_concurrent_sessions: 4\nsummary_model: \"a\\\"b\\\\c\"\nlua_config: true\n")
	out, _, err := runFmt(t, "text", func() error { return configLuaExport(nil) })
	require.NoError(t, err)
	require.Contains(t, out, `rex.set("spinner", "moon")`)
	require.Contains(t, out, `rex.set("reduce_motion", true)`)
	require.Contains(t, out, `rex.set("max_concurrent_sessions", 4)`)
	require.Contains(t, out, `rex.set("summary_model", "a\"b\\c")`)
	require.NotContains(t, out, `"theme"`, "only config.yaml values")
	require.NotContains(t, out, `"lua_config"`, "the flag cannot be set from Lua")

	// What it prints evaluates to the same values.
	dir, err := paths.ConfigDir()
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.lua"), []byte(out), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"), nil, 0o600))
	luacfg.ResetMemo()
	s, rep, _, err := settings.LoadDefaultReport()
	require.NoError(t, err)
	require.NoError(t, rep.Err)
	require.Empty(t, rep.Warnings)
	require.Equal(t, "moon", s.Get("spinner"))
	require.Equal(t, `a"b\c`, s.Get("summary_model"))
	require.Equal(t, 4, s.Get("max_concurrent_sessions"))
}

func TestLuaQuote(t *testing.T) {
	require.Equal(t, `"a\"b\\c\n\r\0001\027x\226\128\174"`, luaQuote("a\"b\\c\n\r\x001\x1bx\u202e"))
}

func TestConfigLuaCommandsRefuseWhenOff(t *testing.T) {
	luaConfigHome(t, "rex.set('theme', 'noir')", "lua_config: false\n")
	for name, run := range map[string]func() error{
		"edit -lua":  func() error { return configEdit([]string{"-lua"}) },
		"lua-export": func() error { return configLuaExport(nil) },
	} {
		_, _, err := runFmt(t, "text", run)
		var ec core.ExitCoder
		require.ErrorAs(t, err, &ec, name)
		require.Equal(t, core.ExitInvalidArgs, ec.ExitCode(), name)
		require.Contains(t, err.Error(), core.OffMessage("lua_config"), name)
	}
}

func TestThemeList_LuaSource(t *testing.T) {
	luaConfigHome(t, "rex.theme('lunar', { extends = 'noir' })\nrex.set('theme', 'lunar')", "")
	out, err := themeRun(t, "agent", func() error { return themeList(nil) })
	require.NoError(t, err)
	require.Contains(t, out, "theme name=lunar source=lua current=true")
}

func TestDoctor_ConfigLuaCheck(t *testing.T) {
	luaConfigHome(t, "rex.set('theme', 'noir')", "")
	c := checkConfigLua()
	require.Equal(t, "config.lua", c.Name)
	require.Equal(t, checkPass, c.Status)

	luaConfigHome(t, "error('\\27[2Jbad')", "")
	c = checkConfigLua()
	require.Equal(t, checkWarn, c.Status)
	require.Contains(t, c.Detail, "bad")
	require.NotContains(t, c.Detail, "\x1b")

	luaConfigHome(t, "rex.set('nope', 1)", "")
	c = checkConfigLua()
	require.Equal(t, checkWarn, c.Status)
	require.Contains(t, c.Detail, "nope")

	luaConfigHome(t, "rex.set('theme', 'noir')", "lua_config: false\n")
	c = checkConfigLua()
	require.Equal(t, checkInfo, c.Status)
	require.Contains(t, c.Detail, "off")

	dir, err := paths.ConfigDir()
	require.NoError(t, err)
	require.NoError(t, os.Remove(filepath.Join(dir, "config.lua")))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"), nil, 0o600))
	luacfg.ResetMemo()
	c = checkConfigLua()
	require.Equal(t, checkInfo, c.Status)
}

// auto and Lua themes side by side: auto stays the built-in-backed "auto"
// row (config.lua cannot define a theme named auto), a Lua theme stays
// "lua", and theme show names config.lua for it.
func TestThemeList_AutoAndLuaThemes(t *testing.T) {
	luaConfigHome(t, "rex.theme('lunar', { extends = 'noir' })\nrex.theme('auto', { extends = 'paper' })\nrex.set('theme', 'auto')", "")
	out, err := themeRun(t, "agent", func() error { return themeList(nil) })
	require.NoError(t, err)
	require.Contains(t, out, "theme name=lunar source=lua current=false")
	require.Contains(t, out, "theme name=auto source=auto current=true")
	require.NotContains(t, out, "name=auto source=lua")

	_, rep, _, err := settings.LoadDefaultReport()
	require.NoError(t, err)
	require.NotContains(t, rep.Themes, "auto")
	require.NotEmpty(t, rep.ThemeIssues)
	require.Contains(t, rep.ThemeIssues[0].Msg, "reserved")

	text, err := themeRun(t, "text", func() error { return themeShow([]string{"lunar"}) })
	require.NoError(t, err)
	require.Contains(t, text, "lunar  config.lua")
	require.NotContains(t, text, "lunar  built-in")
}
