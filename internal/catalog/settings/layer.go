package settings

import (
	"errors"
	"slices"
	"strings"
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
	// FailClosed lists trust settings the layer refused a value for: they
	// read their defaults, whatever config.yaml says.
	FailClosed []string
}

// Layer evaluates the user's Lua config on demand. It must be cheap when
// nothing changed (luacfg memoises by file stamp).
type Layer func() LayerReport

// installed is the process's layer and the stamp of its files.
type installed struct {
	eval  Layer
	stamp func() string
}

var layer atomic.Pointer[installed]

// SetLayer installs the process's layer without a stamp; nil removes it.
// cmd/rex and cmd/rex-daemon install luacfg's layer once at start (with
// SetLayerStamped); tests that do not install one see YAML only.
func SetLayer(l Layer) { SetLayerStamped(l, nil) }

// SetLayerStamped installs the process's layer and stamp, a function whose
// result changes when the layer's files change (reloaders poll it). A nil
// layer removes both.
func SetLayerStamped(l Layer, stamp func() string) {
	if l == nil {
		layer.Store(nil)
		return
	}
	layer.Store(&installed{eval: l, stamp: stamp})
}

// LayerStamp returns the installed layer's stamp, or "" when no layer or no
// stamp is installed or luaOn (lua_config, LuaConfigOn) is false. With it
// false nothing is stat'ed. Callers pass the flag rather than a store so a
// background reader never touches a store the UI goroutine owns.
func LayerStamp(luaOn bool) string {
	l := layer.Load()
	if l == nil || l.stamp == nil || !luaOn {
		return ""
	}
	return l.stamp()
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
		rep = l.eval()
		s.applyLayer(rep)
	}
	return s, rep, err
}

// applyLayer overlays rep. When the layer failed, the trust settings fall
// back to their registry defaults whatever config.yaml says (fail closed:
// the file may be the one that revokes trust), and only the last good
// values stay on top. The ids in rep.FailClosed (a refused rex.set) fall
// back the same way.
func (s *Store) applyLayer(rep LayerReport) {
	s.Overlay(rep.Values, rep.SetAt)
	closed := rep.FailClosed
	if rep.Err != nil {
		closed = TrustSettingIDs
	}
	for _, id := range closed {
		if def, ok := Find(id); ok {
			s.values[id] = def.Default
			s.src[id] = SourceDefault
			delete(s.setAt, id)
		}
	}
}

// StackLayers returns one layer that evaluates layers in order, each on top
// of the ones before it: a later layer's values, locations and themes win
// (a workspace .rex/config.lua over the user's config.lua), and a later
// layer's FailClosed removes an earlier layer's value. Warnings and
// theme issues add up. Any layer's error is the stack's error, so the trust
// settings fail closed whichever file broke. The reports are not changed.
func StackLayers(layers ...Layer) Layer {
	return func() LayerReport {
		var out LayerReport
		var errs []error
		for _, l := range layers {
			r := l()
			out.Exists = out.Exists || r.Exists
			for id, v := range r.Values {
				if out.Values == nil {
					out.Values, out.SetAt = map[string]any{}, map[string]string{}
				}
				out.Values[id], out.SetAt[id] = v, r.SetAt[id]
				out.FailClosed = slices.DeleteFunc(out.FailClosed, func(c string) bool { return c == id })
			}
			for _, id := range r.FailClosed {
				delete(out.Values, id)
				delete(out.SetAt, id)
				if !slices.Contains(out.FailClosed, id) {
					out.FailClosed = append(out.FailClosed, id)
				}
			}
			for n, p := range r.Themes {
				if out.Themes == nil {
					out.Themes = map[string]theme.Palette{}
				}
				out.Themes[n] = p
			}
			out.ThemeIssues = append(out.ThemeIssues, r.ThemeIssues...)
			out.Warnings = append(out.Warnings, r.Warnings...)
			if r.Err != nil {
				errs = append(errs, r.Err)
				out.LastGood = out.LastGood || r.LastGood
			}
		}
		out.Err = errors.Join(errs...)
		return out
	}
}

// StackStamps is the stamp of a StackLayers stack: the stamps in order.
// A nil stamp counts as empty.
func StackStamps(stamps ...func() string) func() string {
	return func() string {
		parts := make([]string, len(stamps))
		for i, s := range stamps {
			if s != nil {
				parts[i] = s()
			}
		}
		return strings.Join(parts, "|")
	}
}
