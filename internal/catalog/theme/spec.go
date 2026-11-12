package theme

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	xansi "github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"
	"gopkg.in/yaml.v3"
)

// Limits on a user theme file. Theme files are shared between people, so
// they are treated as untrusted data.
const (
	// MaxFileSize is the largest theme file read, in bytes.
	MaxFileSize = 64 << 10
	// MaxFiles is the most theme files loaded from one directory.
	MaxFiles = 64
	// MaxFleet is the most colors in a fleet list.
	MaxFleet = 16
)

// Glyphs overrides the state marker characters. An empty field keeps the
// TUI's built-in marker.
type Glyphs struct {
	Needs, Done, Failed, Crashed, Queued, Select, Loop string
}

// spec is the YAML shape of a theme file.
//
//	extends: noir            # optional built-in; default "default"
//	dark: true               # optional
//	colors: { accent: "#FF7A59", fleet: ["#..", "#.."] }
//	glyphs: { needs: "!", done: "+" }
//
// Parse walks a generic map rather than a struct, so an unknown key is a
// warning (a theme written for a newer rex still loads) and a wrong value
// type is a clear error.

// colorKeys maps each color token to its Palette field.
func colorKeys(p *Palette) map[string]*string {
	return map[string]*string{
		"bg": &p.Bg, "fg": &p.Fg, "fg_dim": &p.FgDim, "fg_muted": &p.FgMuted,
		"bg_sel": &p.BgSel, "border": &p.Border, "border_focus": &p.BorderFocus,
		"accent":  &p.Accent,
		"working": &p.Working, "queued": &p.Queued, "needs": &p.Needs,
		"done": &p.Done, "failed": &p.Failed, "crashed": &p.Crashed,
		"warn": &p.Warn, "danger": &p.Danger, "success": &p.Success, "info": &p.Info,
	}
}

func glyphKeys(g *Glyphs) map[string]*string {
	return map[string]*string{
		"needs": &g.Needs, "done": &g.Done, "failed": &g.Failed,
		"crashed": &g.Crashed, "queued": &g.Queued, "select": &g.Select, "loop": &g.Loop,
	}
}

// Token is one named value of a palette.
type Token struct{ Name, Value string }

// colorOrder is the order colors are listed in.
var colorOrder = []string{
	"bg", "fg", "fg_dim", "fg_muted", "bg_sel", "border", "border_focus", "accent",
	"working", "queued", "needs", "done", "failed", "crashed",
	"warn", "danger", "success", "info",
}

// glyphOrder is the order glyphs are listed in.
var glyphOrder = []string{"needs", "done", "failed", "crashed", "queued", "select", "loop"}

// ColorList returns the palette's colors (not the fleet) in display order.
func (p Palette) ColorList() []Token {
	fields := colorKeys(&p)
	out := make([]Token, 0, len(colorOrder))
	for _, k := range colorOrder {
		out = append(out, Token{Name: k, Value: *fields[k]})
	}
	return out
}

// GlyphList returns the palette's glyph overrides in display order; an empty
// value means the built-in marker.
func (p Palette) GlyphList() []Token {
	fields := glyphKeys(&p.Glyphs)
	out := make([]Token, 0, len(glyphOrder))
	for _, k := range glyphOrder {
		out = append(out, Token{Name: k, Value: *fields[k]})
	}
	return out
}

// ColorTokens returns the color token names a theme file may set, sorted,
// plus "fleet".
func ColorTokens() []string {
	var p Palette
	out := []string{"fleet"}
	for k := range colorKeys(&p) {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// GlyphTokens returns the glyph token names a theme file may set, sorted.
func GlyphTokens() []string {
	var g Glyphs
	out := make([]string, 0, 6)
	for k := range glyphKeys(&g) {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Parse reads one theme file's bytes into a palette named name. It returns
// warnings (unknown keys, low contrast) and an error for anything that makes
// the file unusable (bad YAML, unknown base, bad color or glyph). name must
// pass ValidName and must not be a built-in.
func Parse(name string, data []byte) (Palette, []string, error) {
	var zero Palette
	if err := ValidName(name); err != nil {
		return zero, nil, err
	}
	if _, ok := Builtin[name]; ok {
		return zero, nil, fmt.Errorf("theme name %q is a built-in; pick another name", name)
	}
	if len(data) > MaxFileSize {
		return zero, nil, fmt.Errorf("file is larger than %d KiB", MaxFileSize>>10)
	}
	if !utf8.Valid(data) {
		return zero, nil, errors.New("file is not valid UTF-8")
	}
	var raw map[string]any
	dec := yaml.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&raw); err != nil && !errors.Is(err, io.EOF) {
		return zero, nil, fmt.Errorf("invalid YAML: %w", err)
	}
	var warns []string

	base := DefaultName
	if v, ok := raw["extends"]; ok {
		s, isStr := v.(string)
		if !isStr {
			return zero, nil, errors.New(`"extends" must be a built-in theme name`)
		}
		if _, ok := Builtin[s]; !ok {
			return zero, nil, fmt.Errorf(`"extends": %q is not a built-in theme (have: %s)`, s, strings.Join(builtinNames(), ", "))
		}
		base = s
	}
	p, _ := Get(base)
	p.Name = name
	if v, ok := raw["dark"]; ok {
		b, isBool := v.(bool)
		if !isBool {
			return zero, nil, errors.New(`"dark" must be true or false`)
		}
		p.Dark = b
	}

	if v, ok := raw["colors"]; ok && v != nil { // a block with every line commented out is null
		m, isMap := v.(map[string]any)
		if !isMap {
			return zero, nil, errors.New(`"colors" must be a mapping of token to color`)
		}
		fields := colorKeys(&p)
		for _, k := range sortedKeys(m) {
			val := m[k]
			if k == "fleet" {
				fleet, err := parseFleet(val)
				if err != nil {
					return zero, nil, err
				}
				p.Fleet = fleet
				continue
			}
			dst, known := fields[k]
			if !known {
				warns = append(warns, fmt.Sprintf("unknown color token %q (ignored)", k))
				continue
			}
			c, err := parseColor(val)
			if err != nil {
				return zero, nil, fmt.Errorf("colors.%s: %w", k, err)
			}
			*dst = c
		}
	}

	if v, ok := raw["glyphs"]; ok && v != nil {
		m, isMap := v.(map[string]any)
		if !isMap {
			return zero, nil, errors.New(`"glyphs" must be a mapping of token to character`)
		}
		fields := glyphKeys(&p.Glyphs)
		for _, k := range sortedKeys(m) {
			dst, known := fields[k]
			if !known {
				warns = append(warns, fmt.Sprintf("unknown glyph token %q (ignored)", k))
				continue
			}
			s, isStr := m[k].(string)
			if !isStr {
				return zero, nil, fmt.Errorf("glyphs.%s: must be a quoted character", k)
			}
			if err := ValidGlyph(s); err != nil {
				return zero, nil, fmt.Errorf("glyphs.%s: %w", k, err)
			}
			*dst = s
		}
	}

	for _, k := range sortedKeys(raw) {
		if !slices.Contains([]string{"extends", "dark", "colors", "glyphs"}, k) {
			warns = append(warns, fmt.Sprintf("unknown key %q (ignored)", k))
		}
	}
	warns = append(warns, contrastWarnings(p)...)
	return p, warns, nil
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func parseFleet(v any) ([]string, error) {
	list, ok := v.([]any)
	if !ok {
		return nil, errors.New(`colors.fleet: must be a list of colors`)
	}
	if len(list) == 0 || len(list) > MaxFleet {
		return nil, fmt.Errorf("colors.fleet: needs 1-%d colors, got %d", MaxFleet, len(list))
	}
	out := make([]string, 0, len(list))
	for i, e := range list {
		c, err := parseColor(e)
		if err != nil {
			return nil, fmt.Errorf("colors.fleet[%d]: %w", i, err)
		}
		out = append(out, c)
	}
	return out, nil
}

// parseColor validates one color value and returns it normalised: "#RRGGBB"
// (a "#RGB" is expanded), an ANSI index "0"-"255", or "" for the terminal's
// own color.
func parseColor(v any) (string, error) {
	switch c := v.(type) {
	case nil:
		return "", errors.New(`empty value: put the color in quotes ("#RRGGBB"), since # starts a YAML comment`)
	case int:
		if c < 0 || c > 255 {
			return "", fmt.Errorf("ANSI index %d is not 0-255", c)
		}
		return strconv.Itoa(c), nil
	case string:
		return NormalizeColor(c)
	}
	return "", fmt.Errorf("a %T is not a color: use \"#RRGGBB\", \"#RGB\", \"0\"-\"255\" or \"\"", v)
}

// NormalizeColor validates a color string (see parseColor) and returns its
// normal form.
func NormalizeColor(s string) (string, error) {
	switch {
	case s == "":
		return "", nil
	case s[0] == '#':
		h := s[1:]
		if len(h) == 3 {
			h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
		}
		if len(h) != 6 {
			return "", fmt.Errorf("%q is not #RGB or #RRGGBB", s)
		}
		for _, r := range h {
			if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
				return "", fmt.Errorf("%q is not a hex color", s)
			}
		}
		return "#" + strings.ToUpper(h), nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 || n > 255 || strconv.Itoa(n) != s {
		return "", fmt.Errorf("%q is not a color: use \"#RRGGBB\", \"#RGB\", \"0\"-\"255\" or \"\"", s)
	}
	return s, nil
}

// ValidGlyph reports whether s is safe to draw as a state marker: exactly one
// printable grapheme exactly one cell wide, with no control, escape, bidi or
// zero-width characters that could move the cursor or hide text.
func ValidGlyph(s string) error {
	if !utf8.ValidString(s) || s == "" {
		return errors.New("must be one printable character")
	}
	for _, r := range s {
		if isBidi(r) {
			return fmt.Errorf("must not contain bidirectional controls (%U)", r)
		}
		if unicode.IsControl(r) || unicode.IsSpace(r) || !unicode.IsPrint(r) && !isJoiner(r) {
			return fmt.Errorf("must be one printable character (%U is not)", r)
		}
	}
	g := uniseg.NewGraphemes(s)
	n := 0
	for g.Next() {
		n++
	}
	if n != 1 {
		return fmt.Errorf("must be one character, got %d", n)
	}
	// The board lays rows out in fixed cells (a one-cell marker column and a
	// three-cell indent), so a wider glyph such as an emoji would split the
	// row.
	if w := xansi.StringWidth(s); w != 1 {
		return fmt.Errorf("must be exactly 1 cell wide, is %d (wide characters such as emoji break the board columns)", w)
	}
	return nil
}

// isJoiner allows the zero-width joiner and variation selectors inside an
// emoji sequence; the single-grapheme check still bounds them.
func isJoiner(r rune) bool {
	return r == 0x200D || (r >= 0xFE00 && r <= 0xFE0F)
}

func isBidi(r rune) bool {
	return r == 0x200E || r == 0x200F || (r >= 0x202A && r <= 0x202E) || (r >= 0x2066 && r <= 0x2069)
}

// contrastWarnings flags colors a reader would struggle to see against the
// theme's own background. It is advice only: a low-contrast theme still loads.
func contrastWarnings(p Palette) []string {
	if p.Bg == "" {
		return nil
	}
	var out []string
	check := func(label, fg string, min float64) {
		if c := Contrast(fg, p.Bg); c > 0 && c < min {
			out = append(out, fmt.Sprintf("low contrast: %s %s on bg %s is %.1f:1 (aim for %.1f:1)", label, fg, p.Bg, c, min))
		}
	}
	check("fg", p.Fg, 4.5)
	check("fg_dim", p.FgDim, 3)
	for _, t := range []struct{ label, c string }{
		{"accent", p.Accent}, {"working", p.Working}, {"needs", p.Needs},
		{"done", p.Done}, {"failed", p.Failed}, {"crashed", p.Crashed},
	} {
		check(t.label, t.c, 3)
	}
	return out
}
