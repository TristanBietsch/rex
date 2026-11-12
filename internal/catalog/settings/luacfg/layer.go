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

// Layer is the settings layer for config.lua: pass it to settings.SetLayer
// once at process start. Each call is memoised by file stamp (Load), so
// repeated settings loads do not re-evaluate. Without a home directory there
// is nothing to evaluate.
func Layer() settings.Layer {
	return func() settings.LayerReport {
		o, err := OptsForHome()
		if err != nil {
			return settings.LayerReport{}
		}
		r := Load(context.Background(), o)
		return settings.LayerReport{
			Exists: r.Exists, Values: r.Values, SetAt: r.SetAt,
			Themes: r.Themes, ThemeIssues: r.ThemeIssues, Warnings: r.Warnings,
			Err: r.Err, LastGood: r.LastGood,
		}
	}
}
