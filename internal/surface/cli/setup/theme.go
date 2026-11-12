package setup

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/catalog/theme"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

// RunTheme dispatches: rex theme {list|show|new}
func RunTheme(args []string) error {
	if len(args) == 0 {
		return core.Errorf(core.ExitInvalidArgs, "rex theme list", "theme: subcommand required (list|show|new)")
	}
	switch args[0] {
	case "list":
		return themeList(args[1:])
	case "show":
		return themeShow(args[1:])
	case "new":
		return themeNew(args[1:])
	default:
		return core.Errorf(core.ExitInvalidArgs, "rex theme list", "theme: unknown subcommand %q (list|show|new)", args[0])
	}
}

// themeSource says where a theme comes from.
const (
	themeBuiltin = "builtin"
	themeFile    = "file"
	// themeAuto is the auto theme: default or paper by the terminal's
	// background. The TUI asks the terminal; the CLI never does, so here it
	// is listed and shown as default.
	themeAuto = "auto"
)

// themeSourceOf says where the named theme comes from.
func themeSourceOf(cat *theme.Catalog, name string) string {
	switch {
	case cat.IsUser(name):
		return themeFile
	case name == theme.AutoName:
		return themeAuto
	default:
		return themeBuiltin
	}
}

// loadThemes reads the user's theme files and the current theme name. Neither
// failing stops the command: an unreadable config means the default theme,
// and a missing or broken themes directory means built-ins only (the skipped
// files are reported).
func loadThemes() (cat *theme.Catalog, dir, current string, err error) {
	dir, err = paths.ThemesDir()
	if err != nil {
		return nil, "", "", core.Errorf(core.ExitGeneric, "", "theme: %w", err)
	}
	current = theme.DefaultName
	if store, _, lerr := settings.LoadDefault(); lerr == nil {
		current = store.String("theme")
	}
	return theme.LoadDir(dir), dir, current, nil
}

// themeNames is every theme name: the built-ins in the registry's order, then
// the user's files.
func themeNames(cat *theme.Catalog) []string {
	s, _ := settings.Find("theme")
	return append(append([]string(nil), s.Options...), cat.UserNames()...)
}

// themeRow is one theme in `rex theme list`.
type themeRow struct {
	Name    string `json:"name"`
	Source  string `json:"source"`
	Current bool   `json:"current"`
	Dark    bool   `json:"dark"`
	Accent  string `json:"accent"`
}

func themeRows(cat *theme.Catalog, current string) []themeRow {
	var rows []themeRow
	for _, name := range themeNames(cat) {
		p, _ := cat.Get(name)
		rows = append(rows, themeRow{Name: name, Source: themeSourceOf(cat, name), Current: name == current, Dark: p.Dark, Accent: p.Accent})
	}
	return rows
}

func issueNotes(cat *theme.Catalog) []core.Block {
	var notes []core.Block
	for _, i := range cat.Issues {
		tone := core.ToneWarn
		if i.Warn {
			tone = core.ToneDim
		}
		notes = append(notes, core.Note{Tone: tone, Text: i.String()})
	}
	return notes
}

func themeListView(rows []themeRow, cat *theme.Catalog, current string) core.View {
	cols := []core.Col{
		{Key: "name", Title: "Name"},
		{Key: "source", Title: "Source"},
		{Key: "current", Title: "Current"},
		{Key: "mode", Title: "Mode"},
		{Key: "accent", Title: "Accent"},
	}
	cells := make([][]core.Cell, len(rows))
	for i, r := range rows {
		cur := core.Cell{Text: "", Raw: false}
		if r.Current {
			cur = core.Cell{Text: "*", Raw: true}
		}
		mode := "light"
		if r.Dark {
			mode = "dark"
		}
		cells[i] = []core.Cell{{Text: r.Name}, {Text: r.Source}, cur, {Text: mode}, {Text: accentText(r.Accent)}}
	}
	v := core.View{
		core.Header{Title: "Themes", Meta: "current: " + current},
		core.Table{Kind: "theme", Cols: cols, Rows: cells},
	}
	v = append(v, issueNotes(cat)...)
	return append(v, core.Hint{Cmd: "rex theme show <name>", Text: "see a theme's colors and glyphs"})
}

func accentText(a string) string {
	if a == "" {
		return "(terminal)"
	}
	return a
}

func themeList(args []string) error {
	flags := flag.NewFlagSet("theme list", flag.ContinueOnError)
	out := core.AddOutputFlags(flags, core.AllFormats)
	pos, err := out.Parse(flags, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return core.Errorf(core.ExitInvalidArgs, "rex theme list", "theme list: unexpected argument %q", pos[0])
	}
	cat, dir, current, err := loadThemes()
	if err != nil {
		return err
	}
	rows := themeRows(cat, current)
	return core.Out().Emit(core.Result{
		Cmd:  "theme",
		Data: rows,
		Meta: map[string]any{"dir": dir, "current": current},
		View: themeListView(rows, cat, current),
	})
}

// themeShowData is the `-o json` payload of theme show.
type themeShowData struct {
	Name     string            `json:"name"`
	Source   string            `json:"source"`
	Path     string            `json:"path,omitempty"`
	Dark     bool              `json:"dark"`
	Colors   map[string]string `json:"colors"`
	Fleet    []string          `json:"fleet"`
	Glyphs   map[string]string `json:"glyphs"`
	Warnings []string          `json:"warnings"`
}

func themeShow(args []string) error {
	flags := flag.NewFlagSet("theme show", flag.ContinueOnError)
	out := core.AddOutputFlags(flags, core.AllFormats)
	pos, err := out.Parse(flags, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return core.Errorf(core.ExitInvalidArgs, "rex theme list", "theme show: <name> required")
	}
	name := pos[0]
	cat, dir, _, err := loadThemes()
	if err != nil {
		return err
	}
	if !slices.Contains(themeNames(cat), name) {
		return core.Errorf(core.ExitInvalidArgs, "rex theme list", "theme show: unknown theme %q", name)
	}
	p, _ := cat.Get(name)
	data := themeShowData{Name: name, Source: themeSourceOf(cat, name), Dark: p.Dark, Fleet: p.Fleet,
		Colors: map[string]string{}, Glyphs: map[string]string{}, Warnings: []string{}}
	if data.Fleet == nil {
		data.Fleet = []string{}
	}
	meta := "built-in"
	if name == theme.AutoName {
		meta = "auto: default on a dark terminal, paper on a light one (the TUI checks; shown here as default)"
	}
	if cat.IsUser(name) {
		data.Path = filepath.Join(dir, name+".yaml")
		meta = data.Path
	}
	for _, i := range cat.Issues {
		if i.Warn && i.File == name+".yaml" {
			data.Warnings = append(data.Warnings, i.Msg)
		}
	}
	colorRows := make([][]core.Cell, 0, 32)
	for _, tok := range p.ColorList() {
		data.Colors[tok.Name] = tok.Value
		colorRows = append(colorRows, []core.Cell{{Text: tok.Name}, {Text: colorText(tok.Value)}})
	}
	colorRows = append(colorRows, []core.Cell{{Text: "fleet"}, {Text: strings.Join(p.Fleet, " ")}})
	glyphRows := make([][]core.Cell, 0, 6)
	for _, g := range p.GlyphList() {
		data.Glyphs[g.Name] = g.Value
		glyphRows = append(glyphRows, []core.Cell{{Text: g.Name}, {Text: glyphText(g.Value)}})
	}
	v := core.View{
		core.Header{Title: name, Meta: meta},
		core.Table{Kind: "color", Cols: tokenCols(), Rows: colorRows},
		core.Section{Title: "Glyphs"},
		core.Table{Kind: "glyph", Cols: tokenCols(), Rows: glyphRows, NoCount: true},
		core.Section{Title: "Sample"},
		core.Pre{Text: themeSample(p)},
	}
	for _, w := range data.Warnings {
		v = append(v, core.Note{Tone: core.ToneWarn, Text: w})
	}
	return core.Out().Emit(core.Result{Cmd: "theme", Data: data, View: v})
}

func tokenCols() []core.Col {
	return []core.Col{{Key: "token", Title: "Token"}, {Key: "value", Title: "Value"}}
}

func colorText(v string) string {
	if v == "" {
		return "(terminal)"
	}
	return v
}

func glyphText(v string) string {
	if v == "" {
		return "(built-in)"
	}
	return v
}

// themeSample is a plain-text board line drawn with the theme's markers.
func themeSample(p theme.Palette) string {
	pick := func(g, def string) string {
		if g != "" {
			return g
		}
		return def
	}
	g := p.Glyphs
	return fmt.Sprintf("%s needs you   ⠋ working   %s queued   %s done   %s failed   %s crashed",
		pick(g.Needs, "◆"), pick(g.Queued, "◌"), pick(g.Done, "●"), pick(g.Failed, "✕"), pick(g.Crashed, "○"))
}

// themeNewData is the `-o json` payload of theme new.
type themeNewData struct {
	Name string `json:"name"`
	Path string `json:"path"`
	From string `json:"from"`
}

func themeNew(args []string) error {
	flags := flag.NewFlagSet("theme new", flag.ContinueOnError)
	out := core.AddOutputFlags(flags, core.AllFormats)
	from := flags.String("from", theme.DefaultName, "built-in theme to start from")
	force := flags.Bool("force", false, "overwrite an existing theme file")
	pos, err := out.Parse(flags, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return core.Errorf(core.ExitInvalidArgs, "rex theme new my-theme", "theme new: <name> required")
	}
	name := pos[0]
	if err := theme.ValidName(name); err != nil {
		return core.Errorf(core.ExitInvalidArgs, "", "theme new: %w", err)
	}
	if _, ok := theme.Builtin[name]; ok || name == theme.AutoName {
		return core.Errorf(core.ExitInvalidArgs, "rex theme new my-"+name, "theme new: %q is a built-in theme name", name)
	}
	base, ok := theme.Builtin[*from]
	if !ok {
		return core.Errorf(core.ExitInvalidArgs, "rex theme list", "theme new: -from %q is not a built-in theme", *from)
	}
	dir, err := paths.ThemesDir()
	if err != nil {
		return core.Errorf(core.ExitGeneric, "", "theme new: %w", err)
	}
	path, err := writeThemeFile(dir, name, themeSkeleton(name, *from, base), *force)
	if err != nil {
		var ee core.ExitError
		if errors.As(err, &ee) {
			return err
		}
		return core.Errorf(core.ExitGeneric, "", "theme new: %w", err)
	}
	line := core.Line{Tone: core.ToneOK, Glyph: core.Out().Glyphs.OK, Text: "created " + path, Txt: path}
	return core.Out().Emit(core.Result{
		Cmd:  "theme",
		Data: themeNewData{Name: name, Path: path, From: *from},
		View: append(core.ActionView(line,
			core.Field{Key: "name", Val: name}, core.Field{Key: "path", Val: path}, core.Field{Key: "from", Val: *from}),
			core.Hint{Cmd: "rex config set theme " + name, Text: "use it"}),
	})
}

// writeThemeFile creates <dir>/<name>.yaml (0600, never following a link;
// dir is 0700 when new) and returns its path. An existing file is refused
// unless force, which replaces it.
func writeThemeFile(dir, name, content string, force bool) (string, error) {
	path := filepath.Join(dir, name+".yaml")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if force {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // G304: path is <themes dir>/<validated name>.yaml
	if errors.Is(err, fs.ErrExist) {
		return "", core.Errorf(core.ExitOperationRefused, "rex theme new "+name+" -force",
			"theme new: %s already exists; pass -force to replace it", path)
	}
	if err != nil {
		return "", err
	}
	_, werr := f.WriteString(content)
	return path, errors.Join(werr, f.Close())
}

// themeSkeleton is a commented theme file: it extends base and lists every
// token with the base's value, commented out, so an edit is a one-line
// uncomment.
func themeSkeleton(name, from string, base theme.Palette) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# rex theme %q. Apply it with: rex config set theme %s\n", name, name)
	b.WriteString("# Uncomment a line to change it; every token you leave alone comes from the base theme.\n")
	b.WriteString("# Colors are \"#RRGGBB\", \"#RGB\", \"0\"-\"255\" (a terminal palette index) or \"\" (the terminal's own).\n")
	b.WriteString("# Always quote a color: an unquoted # starts a YAML comment.\n\n")
	fmt.Fprintf(&b, "extends: %s\n", from)
	fmt.Fprintf(&b, "# dark: %t\n\n", base.Dark)
	b.WriteString("# colors:\n")
	for _, tok := range base.ColorList() {
		fmt.Fprintf(&b, "#   %s: %q\n", tok.Name, tok.Value)
	}
	if len(base.Fleet) > 0 {
		quoted := make([]string, len(base.Fleet))
		for i, c := range base.Fleet {
			quoted[i] = fmt.Sprintf("%q", c)
		}
		fmt.Fprintf(&b, "#   fleet: [%s]\n", strings.Join(quoted, ", "))
	}
	b.WriteString("\n# glyphs: one printable character, exactly one cell wide, each (the values below are the built-in markers)\n# glyphs:\n")
	b.WriteString("#   needs: \"◆\"\n#   done: \"●\"\n#   failed: \"✕\"\n#   crashed: \"○\"\n#   queued: \"◌\"\n#   select: \"▸\"\n#   loop: \"⟳\"\n")
	return b.String()
}
