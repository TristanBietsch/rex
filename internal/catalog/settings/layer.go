package settings

import (
	"sync/atomic"

	"github.com/tristanbietsch/rex/internal/catalog/theme"
)

// LayerReport is what the process's layer (config.lua) produced: the values
// to put on top of config.yaml, plus its themes and problems. The zero value
// means no layer ran.
type LayerReport struct {
	// Exists is true when the layer's file exists.
	Exists bool
	// Values and SetAt are the validated overlay (id to value, id to
	// "config.lua:12").
	Values map[string]any
	SetAt  map[string]string
	// Themes and ThemeIssues are the themes the layer defined.
	Themes      map[string]theme.Palette
	ThemeIssues []theme.Issue
	// Warnings are refused calls and the like. All text is safe to print.
	Warnings []string
	// Err is why the file did not evaluate. When it is set, the trust
	// settings read their defaults (fail closed), and Values hold only the
	// last good evaluation, if LastGood.
	Err      error
	LastGood bool
}

// Layer evaluates the user's Lua config on demand. It must be cheap when
// nothing changed (luacfg memoises by file stamp).
type Layer func() LayerReport

var layer atomic.Pointer[Layer]

// SetLayer installs the process's layer; nil removes it. cmd/rex and
// cmd/rex-daemon install luacfg's layer once at start; tests that do not
// install one see YAML only.
func SetLayer(l Layer) {
	if l == nil {
		layer.Store(nil)
		return
	}
	layer.Store(&l)
}

// LuaConfigOn reports whether lua_config is on in the YAML-only store s.
func LuaConfigOn(s *Store) bool { return FeatureOn(s, "lua_config") }

// LoadLayered loads config.yaml from path, then, when a layer is installed
// and lua_config is on in the YAML values, evaluates the layer and puts its
// values on top (SourceLua). With lua_config off the layer is never called:
// no stat, read or evaluation of config.lua happens. The error is the YAML
// load's; the Store always holds at least the defaults.
func LoadLayered(path string) (*Store, LayerReport, error) {
	s := NewStore()
	err := s.Load(path)
	var rep LayerReport
	if l := layer.Load(); l != nil && LuaConfigOn(s) {
		rep = (*l)()
		s.applyLayer(rep)
	}
	return s, rep, err
}

// applyLayer overlays rep. When the layer failed, the trust settings fall
// back to their registry defaults whatever config.yaml says (fail closed:
// the file may be the one that revokes trust), and only the last good
// values stay on top.
func (s *Store) applyLayer(rep LayerReport) {
	s.Overlay(rep.Values, rep.SetAt)
	if rep.Err == nil {
		return
	}
	for _, id := range TrustSettingIDs {
		if def, ok := Find(id); ok {
			s.values[id] = def.Default
			s.src[id] = SourceDefault
			delete(s.setAt, id)
		}
	}
}
