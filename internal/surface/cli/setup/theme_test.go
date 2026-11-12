package setup

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/catalog/theme"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/testutil/testperm"
)

// themeHome isolates HOME and returns the themes directory.
func themeHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	dir, err := paths.ThemesDir()
	require.NoError(t, err)
	return dir
}

func writeUserTheme(t *testing.T, dir, name, body string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, name+".yaml"), []byte(body), 0o600))
}

// themeRun runs fn with a Printer in format whose stdout and stderr are
// buffers, and returns what each got.
func themeRun(t *testing.T, format string, fn func() error) (stdout string, err error) {
	t.Helper()
	var out, errw bytes.Buffer
	env := core.Env{Getenv: func(string) string { return "" }, Width: 80, Scheme: "default", GOOS: "linux"}
	p, perr := core.NewPrinter(core.FormatFlags{Output: format}, env, &out, &errw)
	require.NoError(t, perr)
	core.SetOut(p)
	t.Cleanup(func() { core.SetOut(nil) })
	err = fn()
	return out.String(), err
}

func requireExit(t *testing.T, err error, code int) {
	t.Helper()
	var ec core.ExitCoder
	require.ErrorAs(t, err, &ec)
	require.Equal(t, code, ec.ExitCode(), err.Error())
}

func TestThemeList(t *testing.T) {
	dir := themeHome(t)
	writeUserTheme(t, dir, "mine", "extends: noir\n")
	writeUserTheme(t, dir, "bad", `colors: {accent: "red"}`)
	require.NoError(t, os.WriteFile(filepath.Join(filepath.Dir(dir), "config.yaml"), []byte("theme: mine\n"), 0o600))

	out, err := themeRun(t, "json", func() error { return themeList(nil) })
	require.NoError(t, err)
	var env struct {
		Data []themeRow     `json:"data"`
		Meta map[string]any `json:"meta"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &env), out)
	var names []string
	for _, r := range env.Data {
		names = append(names, r.Name)
		if r.Name == "mine" {
			require.Equal(t, themeFile, r.Source)
			require.True(t, r.Current)
			require.True(t, r.Dark)
		} else {
			require.False(t, r.Current, r.Name)
		}
	}
	require.Equal(t, append(append([]string(nil), mustTheme(t).Options...), "mine"), names, "built-ins first, in registry order; a skipped file is not listed")
	require.Equal(t, dir, env.Meta["dir"])

	text, err := themeRun(t, "text", func() error { return themeList(nil) })
	require.NoError(t, err)
	require.Contains(t, text, "Themes")
	require.Contains(t, text, `"bad.yaml": colors.accent`, "a skipped file is reported")
	require.Contains(t, text, "rex theme show <name>")

	agent, err := themeRun(t, "agent", func() error { return themeList(nil) })
	require.NoError(t, err)
	require.Contains(t, agent, "theme name=default source=builtin")
	require.Contains(t, agent, "theme name=mine source=file current=true")
}

func mustTheme(t *testing.T) settings.Setting {
	t.Helper()
	s, ok := settings.Find("theme")
	require.True(t, ok)
	return s
}

func TestThemeList_RejectsArguments(t *testing.T) {
	themeHome(t)
	_, err := themeRun(t, "text", func() error { return themeList([]string{"extra"}) })
	requireExit(t, err, core.ExitInvalidArgs)
}

func TestThemeShow(t *testing.T) {
	dir := themeHome(t)
	writeUserTheme(t, dir, "mine", "extends: noir\nsparkle: 1\nglyphs: {needs: \"!\"}\n")

	out, err := themeRun(t, "json", func() error { return themeShow([]string{"mine"}) })
	require.NoError(t, err)
	var env struct {
		Data themeShowData `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &env), out)
	d := env.Data
	require.Equal(t, "mine", d.Name)
	require.Equal(t, themeFile, d.Source)
	require.Equal(t, filepath.Join(dir, "mine.yaml"), d.Path)
	require.Equal(t, theme.Builtin["noir"].Accent, d.Colors["accent"])
	require.Equal(t, "!", d.Glyphs["needs"])
	require.Equal(t, "", d.Glyphs["done"])
	require.NotEmpty(t, d.Fleet)
	require.Equal(t, []string{`unknown key "sparkle" (ignored)`}, d.Warnings)

	text, err := themeRun(t, "text", func() error { return themeShow([]string{"mine"}) })
	require.NoError(t, err)
	require.Contains(t, text, "accent")
	require.Contains(t, text, "! ready")
	require.Contains(t, text, "(built-in)", "an unset glyph says so")

	out, err = themeRun(t, "json", func() error { return themeShow([]string{"ansi"}) })
	require.NoError(t, err)
	var builtin struct {
		Data themeShowData `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &builtin))
	require.Equal(t, themeBuiltin, builtin.Data.Source)
	require.Empty(t, builtin.Data.Path)
	require.NotNil(t, builtin.Data.Fleet, "the field is never null")
	text, err = themeRun(t, "text", func() error { return themeShow([]string{"ansi"}) })
	require.NoError(t, err)
	require.Contains(t, text, "(terminal)", "an empty color is the terminal's own")
}

func TestThemeShow_Errors(t *testing.T) {
	themeHome(t)
	for _, args := range [][]string{nil, {"a", "b"}, {"ghost"}} {
		_, err := themeRun(t, "text", func() error { return themeShow(args) })
		requireExit(t, err, core.ExitInvalidArgs)
	}
}

func TestThemeNew(t *testing.T) {
	dir := themeHome(t)
	out, err := themeRun(t, "json", func() error { return themeNew([]string{"my-theme", "-from", "noir"}) })
	require.NoError(t, err)
	var env struct {
		Data themeNewData `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &env), out)
	path := filepath.Join(dir, "my-theme.yaml")
	require.Equal(t, themeNewData{Name: "my-theme", Path: path, From: "noir"}, env.Data)
	testperm.RequireMode(t, path, 0o600)
	testperm.RequireMode(t, dir, 0o700)

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(raw), "extends: noir")
	p, warns, err := theme.Parse("my-theme", raw)
	require.NoError(t, err, "the skeleton is a valid theme")
	require.Empty(t, warns)
	want := theme.Builtin["noir"]
	want.Name = "my-theme"
	require.Equal(t, want, p, "an untouched skeleton is its base")

	// It shows up in the list.
	list, err := themeRun(t, "agent", func() error { return themeList(nil) })
	require.NoError(t, err)
	require.Contains(t, list, "theme name=my-theme source=file")
}

// A user file named after a bundled theme overrides it: listed once, as a
// file, in the bundled theme's registry slot.
func TestThemeList_UserOverridesBundled(t *testing.T) {
	dir := themeHome(t)
	writeUserTheme(t, dir, "dracula", `colors: {accent: "#123456"}`)
	writeUserTheme(t, dir, "mine", "")

	out, err := themeRun(t, "json", func() error { return themeList(nil) })
	require.NoError(t, err)
	var env struct {
		Data []themeRow `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &env), out)
	var names []string
	for _, r := range env.Data {
		names = append(names, r.Name)
		switch r.Name {
		case "dracula":
			require.Equal(t, themeFile, r.Source)
			require.Equal(t, "#123456", r.Accent)
		case "tokyo_night":
			require.Equal(t, themeBuiltin, r.Source)
		}
	}
	require.Equal(t, append(append([]string(nil), mustTheme(t).Options...), "mine"), names)
}

// `rex theme new dracula` writes an override; -from still takes only a Go
// built-in.
func TestThemeNew_BundledNameMakesAnOverride(t *testing.T) {
	dir := themeHome(t)
	_, err := themeRun(t, "text", func() error { return themeNew([]string{"dracula", "-from", "noir"}) })
	require.NoError(t, err)
	require.FileExists(t, filepath.Join(dir, "dracula.yaml"))
}

func TestThemeNew_RefusesOverwriteWithoutForce(t *testing.T) {
	dir := themeHome(t)
	writeUserTheme(t, dir, "mine", "# precious\n")
	_, err := themeRun(t, "text", func() error { return themeNew([]string{"mine"}) })
	requireExit(t, err, core.ExitOperationRefused)
	require.ErrorContains(t, err, "-force")
	raw, rerr := os.ReadFile(filepath.Join(dir, "mine.yaml"))
	require.NoError(t, rerr)
	require.Equal(t, "# precious\n", string(raw))

	_, err = themeRun(t, "text", func() error { return themeNew([]string{"mine", "-force"}) })
	require.NoError(t, err)
	raw, rerr = os.ReadFile(filepath.Join(dir, "mine.yaml"))
	require.NoError(t, rerr)
	require.Contains(t, string(raw), "extends: default")
	testperm.RequireMode(t, filepath.Join(dir, "mine.yaml"), 0o600)
}

func TestThemeNew_ForceReplacesALinkInsteadOfFollowingIt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	dir := themeHome(t)
	require.NoError(t, os.MkdirAll(dir, 0o700))
	target := filepath.Join(t.TempDir(), "target.txt")
	require.NoError(t, os.WriteFile(target, []byte("keep"), 0o600))
	require.NoError(t, os.Symlink(target, filepath.Join(dir, "mine.yaml")))

	_, err := themeRun(t, "text", func() error { return themeNew([]string{"mine", "-force"}) })
	require.NoError(t, err)
	raw, rerr := os.ReadFile(target)
	require.NoError(t, rerr)
	require.Equal(t, "keep", string(raw), "the link's target is untouched")
	fi, rerr := os.Lstat(filepath.Join(dir, "mine.yaml"))
	require.NoError(t, rerr)
	require.True(t, fi.Mode().IsRegular())
}

func TestThemeNew_BadArguments(t *testing.T) {
	themeHome(t)
	for name, args := range map[string][]string{
		"no name":       nil,
		"two names":     {"a", "b"},
		"bad name":      {"Not Valid"},
		"path name":     {"../x"},
		"builtin name":  {"noir"},
		"auto name":     {"auto"},
		"bad -from":     {"mine", "-from", "ghost"},
		"bundled -from": {"mine", "-from", "dracula"},
		"user -from":    {"mine", "-from", "mine"},
		"unknown flag":  {"mine", "-bogus"},
	} {
		_, err := themeRun(t, "text", func() error { return themeNew(args) })
		requireExit(t, err, core.ExitInvalidArgs)
		_ = name
	}
}

func TestThemeNew_CannotCreateDirectory(t *testing.T) {
	dir := themeHome(t)
	require.NoError(t, os.MkdirAll(filepath.Dir(dir), 0o700))
	require.NoError(t, os.WriteFile(dir, []byte("a file where the directory goes"), 0o600))
	_, err := themeRun(t, "text", func() error { return themeNew([]string{"mine"}) })
	requireExit(t, err, core.ExitGeneric)
}

func TestRunTheme_Dispatch(t *testing.T) {
	themeHome(t)
	_, err := themeRun(t, "text", func() error { return RunTheme(nil) })
	requireExit(t, err, core.ExitInvalidArgs)
	_, err = themeRun(t, "text", func() error { return RunTheme([]string{"nope"}) })
	requireExit(t, err, core.ExitInvalidArgs)
	for _, sub := range []string{"list", "show default", "new via-dispatch"} {
		args := append([]string{}, splitWords(sub)...)
		_, err = themeRun(t, "text", func() error { return RunTheme(args) })
		require.NoError(t, err, sub)
	}
}

func splitWords(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == ' ' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	return append(out, cur)
}

func TestThemeList_SurvivesABrokenConfig(t *testing.T) {
	dir := themeHome(t)
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(filepath.Dir(dir), "config.yaml"), []byte("spinner: [unclosed\n"), 0o600))
	out, err := themeRun(t, "text", func() error { return themeList(nil) })
	require.NoError(t, err)
	require.Contains(t, out, "current: default")
}

func TestThemeList_HostileFilesNeverPutEscapesInOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows file names cannot hold control characters")
	}
	dir := themeHome(t)
	writeUserTheme(t, dir, "a\x1b[2Jb", "colors: {}")
	writeUserTheme(t, dir, "ok", `colors: {fleet: ["\u001b[2J"]}`)
	for _, format := range []string{"text", "txt", "md", "json", "agent"} {
		out, err := themeRun(t, format, func() error { return themeList(nil) })
		require.NoError(t, err, format)
		require.NotContains(t, out, "\x1b", format)
		require.Contains(t, out, "default", format)
	}
}

func TestThemeList_AutoIsListedAsTheDarkDefault(t *testing.T) {
	dir := themeHome(t)
	require.NoError(t, os.MkdirAll(filepath.Dir(dir), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(filepath.Dir(dir), "config.yaml"), []byte("theme: auto\n"), 0o600))

	out, err := themeRun(t, "json", func() error { return themeList(nil) })
	require.NoError(t, err)
	var env struct {
		Data []themeRow `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &env), out)
	var auto *themeRow
	for i := range env.Data {
		if env.Data[i].Name == theme.AutoName {
			auto = &env.Data[i]
		}
	}
	require.NotNil(t, auto, "auto is listed")
	require.Equal(t, themeAuto, auto.Source)
	require.True(t, auto.Current)
	require.True(t, auto.Dark, "the CLI never asks the terminal: auto is the dark default")
	require.Equal(t, theme.Builtin[theme.DefaultName].Accent, auto.Accent)

	text, err := themeRun(t, "text", func() error { return themeShow([]string{"auto"}) })
	require.NoError(t, err)
	require.Contains(t, text, "paper on a light one")
}
