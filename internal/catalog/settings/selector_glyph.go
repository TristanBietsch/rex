package settings

// SelectorTheme is the selector_glyph value that keeps the theme's own
// selection marker.
const SelectorTheme = "theme"

// normalizeSelectorGlyph is the Normalize func of selector_glyph: theme, or
// one glyph under the same rules as prompt_glyph (the selector is drawn in a
// fixed-width gutter, so it is one printable grapheme, one or two cells).
func normalizeSelectorGlyph(v string) (string, error) {
	if v == SelectorTheme {
		return v, nil
	}
	return normalizePromptGlyph(v)
}
