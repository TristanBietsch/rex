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
var Bundled = map[string]Palette{}

// init fills Bundled (not a var initializer: Parse reaches Names, which reads
// Bundled, and Go rejects that cycle). A bundled file must parse with no
// error and no warning; anything else is a build mistake, so it panics.
func init() {
	files, err := fs.Glob(bundledFS, "builtin/*.yaml")
	if err != nil {
		panic(fmt.Sprintf("theme: bundled themes: %v", err))
	}
	for _, f := range files {
		data, err := bundledFS.ReadFile(f)
		if err != nil {
			panic(fmt.Sprintf("theme: bundled %s: %v", f, err))
		}
		name := strings.TrimSuffix(path.Base(f), ".yaml")
		p, warns, err := Parse(name, data)
		if err != nil {
			panic(fmt.Sprintf("theme: bundled %s: %v", f, err))
		}
		if len(warns) > 0 {
			panic(fmt.Sprintf("theme: bundled %s: %s", f, strings.Join(warns, "; ")))
		}
		Bundled[name] = p
	}
}
