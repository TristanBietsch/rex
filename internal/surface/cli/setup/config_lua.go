package setup

import (
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

// luaTemplate is the commented config.lua that `rex config edit -lua`
// writes when there is none.
//
//go:embed example_config.lua
var luaTemplate string

// requireLuaConfig is the exit-5 refusal of the config.lua commands while
// lua_config is off.
func requireLuaConfig() error { return core.RequireFeature("lua_config") }

// ensureLuaConfig creates config.lua from the template when it does not
// exist: 0600 in a 0700 directory, never over an existing file.
func ensureLuaConfig() (string, error) {
	l, err := paths.Resolve()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(l.LuaConfigFile), 0o700); err != nil {
		return "", err
	}
	f, err := os.OpenFile(l.LuaConfigFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	switch {
	case errors.Is(err, fs.ErrExist):
		return l.LuaConfigFile, nil
	case err != nil:
		return "", err
	}
	if _, err := f.WriteString(luaTemplate); err != nil {
		_ = f.Close()
		return "", err
	}
	return l.LuaConfigFile, f.Close()
}

// configLuaExport is `rex config lua-export`: the values config.yaml holds,
// printed as a config.lua to start from. It never writes a file.
func configLuaExport(args []string) error {
	fs := flag.NewFlagSet("config lua-export", flag.ContinueOnError)
	out := core.AddOutputFlags(fs, core.AllFormats)
	rest, err := out.Parse(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 0 {
		return core.NewExitError(core.ExitInvalidArgs, "config lua-export: takes no arguments")
	}
	if err := requireLuaConfig(); err != nil {
		return err
	}
	path, err := paths.ConfigFile()
	if err != nil {
		return core.NewExitError(core.ExitGeneric, err.Error())
	}
	yaml := settings.NewStore() // config.yaml only, without config.lua on top
	if err := yaml.Load(path); err != nil {
		return core.NewExitError(core.ExitGeneric, err.Error())
	}
	body := luaExport(yaml)
	return core.Out().Emit(core.Result{
		Cmd: "config",
		Data: struct {
			Lua string `json:"lua"`
		}{body},
		Content: body,
	})
}

// luaExport writes one rex.set line per setting config.yaml holds, in
// registry order. lua_config and read-only settings are left out: Lua may
// not set them.
func luaExport(s *settings.Store) string {
	var b strings.Builder
	b.WriteString("-- config.lua exported from config.yaml by `rex config lua-export`.\n")
	b.WriteString("-- Values set here win over config.yaml. Docs: docs/howto/lua-config.md\n\n")
	for _, st := range settings.Registry {
		if s.Source(st.ID) != settings.SourceFile || st.ReadOnly || st.ID == "lua_config" {
			continue
		}
		fmt.Fprintf(&b, "rex.set(%s, %s)\n", luaQuote(st.ID), luaValue(s.Get(st.ID)))
	}
	return b.String()
}

func luaValue(v any) string {
	switch x := v.(type) {
	case bool:
		return strconv.FormatBool(x)
	case int:
		return strconv.Itoa(x)
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64)
	case string:
		return luaQuote(x)
	}
	return luaQuote(fmt.Sprint(v))
}

// luaQuote is s as a Lua string literal: quotes and backslashes escaped,
// common controls as \n \r \t, and every other byte outside printable ASCII
// as a three-digit decimal escape (\ddd, so a following digit cannot join
// it). The literal is plain ASCII and reads back as the same bytes.
func luaQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"' || c == '\\':
			b.WriteByte('\\')
			b.WriteByte(c)
		case c == '\n':
			b.WriteString(`\n`)
		case c == '\r':
			b.WriteString(`\r`)
		case c == '\t':
			b.WriteString(`\t`)
		case c < 0x20 || c >= 0x7f:
			fmt.Fprintf(&b, `\%03d`, c)
		default:
			b.WriteByte(c)
		}
	}
	b.WriteByte('"')
	return b.String()
}
