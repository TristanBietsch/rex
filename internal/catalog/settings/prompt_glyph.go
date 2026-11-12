package settings

import (
	"errors"
	"fmt"
	"unicode"
	"unicode/utf8"

	"github.com/rivo/uniseg"
)

// normalizePromptGlyph is the Normalize func of prompt_glyph. The glyph is
// drawn on every frame, so it must be exactly one printable grapheme (one
// user-perceived character, combining marks included) that is one or two
// cells wide. Control characters, bidi marks, zero-width characters and
// spaces are refused: they would break the prompt line or hide text.
func normalizePromptGlyph(v string) (string, error) {
	if !utf8.ValidString(v) {
		return "", errors.New("must be valid text")
	}
	if v == "" {
		return "", errors.New("must be one printable character")
	}
	for _, r := range v {
		if unicode.IsSpace(r) || !unicode.IsPrint(r) {
			return "", fmt.Errorf("must be one printable character (%U is not)", r)
		}
	}
	graphemes := uniseg.NewGraphemes(v)
	n := 0
	for graphemes.Next() {
		n++
	}
	if n != 1 {
		return "", fmt.Errorf("must be one character, got %d", n)
	}
	if w := uniseg.StringWidth(v); w < 1 || w > 2 {
		return "", fmt.Errorf("must be 1 or 2 cells wide, got %d", w)
	}
	return v, nil
}
