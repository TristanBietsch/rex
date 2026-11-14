package luacfg

import (
	"context"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
)

// OptsForHome returns the evaluation options for the current user's
// ~/.config/rex/config.lua. It fails when the home directory is unknown.
func OptsForHome() (Opts, error) {
	l, err := paths.Resolve()
	if err != nil {
		return Opts{}, err
	}
	return Opts{File: l.LuaConfigFile, ModuleDir: l.LuaModuleDir}, nil
}

// Layer is the settings layer for the user's config.lua (OptsForHome).
func Layer() settings.Layer { return LayerFor(OptsForHome) }

// LayerFor is the settings layer for the file opts names: pass it to
// settings.SetLayer, or stack it (settings.StackLayers) with another
// scope's layer. Each call is memoised by file stamp (Load), so repeated
// settings loads do not re-evaluate. When opts fails (no home directory)
// there is nothing to evaluate.
func LayerFor(opts func() (Opts, error)) settings.Layer {
	return func() settings.LayerReport {
		o, err := opts()
		if err != nil {
			return settings.LayerReport{}
		}
		r := Load(context.Background(), o)
		return settings.LayerReport{
			Exists: r.Exists, Values: r.Values, SetAt: r.SetAt, Given: r.Given,
			Themes: r.Themes, ThemeIssues: r.ThemeIssues, Warnings: r.Warnings,
			Err: r.Err, LastGood: r.LastGood, FailClosed: r.FailClosed,
		}
	}
}

// StampFor is Stamp for the files opts names, "" when opts fails.
func StampFor(opts func() (Opts, error)) func() string {
	return func() string {
		o, err := opts()
		if err != nil {
			return ""
		}
		return Stamp(o)
	}
}

// HomeStamp is Stamp for the current user's config.lua and lua/, or "" when
// the home directory is unknown.
func HomeStamp() string { return StampFor(OptsForHome)() }

// Install makes the user's config.lua the settings layer of this process,
// with its file stamp for reloaders. Call it once at process start.
func Install() { settings.SetLayerStamped(Layer(), HomeStamp) }
