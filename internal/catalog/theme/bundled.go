package theme

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"strings"
)

// bundledFS holds the theme files that ship with rex. They use the user theme
// format, so they are a worked example of it.
//
//go:embed builtin/*.yaml
var bundledFS embed.FS

// Bundled maps the bundled theme names to palettes parsed from builtin/*.yaml
// at init. Unlike the Go built-ins they are not bases: "extends:" and
// `rex theme new -from` accept only Builtin names, and a user file with a
// bundled name overrides the bundled theme. Treat it as read-only; use Get
// for a copy that is safe to modify.
var Bundled map[string]Palette

// init fills Bundled (not a var initializer: Parse reaches Names, which reads
// Bundled, and Go rejects that cycle).
func init() { Bundled = mustBundled(loadBundled(bundledFS)) }

// loadBundled parses every builtin/*.yaml in fsys. A bundled file must parse
// with no error and no warning; anything else is an error.
func loadBundled(fsys fs.FS) (map[string]Palette, error) {
	// The only Glob error is a bad pattern, and this one is constant.
	files, _ := fs.Glob(fsys, "builtin/*.yaml")
	out := make(map[string]Palette, len(files))
	for _, f := range files {
		data, err := fs.ReadFile(fsys, f)
		if err != nil {
			return nil, fmt.Errorf("bundled %s: %w", f, err)
		}
		name := strings.TrimSuffix(path.Base(f), ".yaml")
		p, warns, err := Parse(name, data)
		if err != nil {
			return nil, fmt.Errorf("bundled %s: %w", f, err)
		}
		if len(warns) > 0 {
			return nil, fmt.Errorf("bundled %s: %s", f, strings.Join(warns, "; "))
		}
		out[name] = p
	}
	return out, nil
}

// mustBundled panics on a loadBundled error: a broken bundled file is a
// build mistake, never a user's.
func mustBundled(m map[string]Palette, err error) map[string]Palette {
	if err != nil {
		panic("theme: " + err.Error())
	}
	return m
}
