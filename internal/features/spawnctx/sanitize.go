package spawnctx

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// Sanitize makes pasted or typed text safe to put in a prompt and to show in
// a terminal:
//
//   - invalid UTF-8 becomes U+FFFD;
//   - "\r\n" and a lone "\r" become "\n";
//   - ANSI escape sequences go whole (ansi.Strip);
//   - every other C0 and C1 control and DEL goes; "\n" and "\t" stay;
//   - bidi controls go: the embeddings and overrides U+202A–U+202E and the
//     isolates U+2066–U+2069 (Trojan Source), and also the marks U+200E,
//     U+200F and U+061C, which the spec does not list but which reorder text
//     the same way;
//   - invisible characters go: the Unicode Tag block U+E0000–U+E007F (the
//     "ASCII smuggling" channel: text a model reads but a terminal never
//     shows), the zero-width U+200B, U+200C, U+200D, U+2060 and U+FEFF, the
//     invisible math operators U+2061–U+2064, and the variation selectors
//     U+FE00–U+FE0F and U+E0100–U+E01EF (runs of them after one visible
//     character hide bytes the same way the Tag block does). Emoji joined
//     with U+200D come apart into their parts, and an emoji written with
//     U+FE0F loses its presentation hint.
//
// The result is valid UTF-8 and Sanitize is idempotent (FuzzSanitize).
func Sanitize(s string) string {
	s = strings.ToValidUTF8(s, "�")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = ansi.Strip(s)
	// Strip can cut a rune that sat inside an escape sequence in half.
	s = strings.ToValidUTF8(s, "�")
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if unicode.IsControl(r) || isBidi(r) || isInvisible(r) {
			return -1
		}
		return r
	}, s)
}

// isBidi reports a bidi control rune Sanitize drops.
func isBidi(r rune) bool {
	switch {
	case r >= 0x202A && r <= 0x202E, r >= 0x2066 && r <= 0x2069:
		return true
	case r == 0x200E, r == 0x200F, r == 0x061C:
		return true
	}
	return false
}

// isInvisible reports a rune Sanitize drops because it draws as nothing
// but still reaches the model: the Tag block, the zero-width characters, the
// invisible math operators and the variation selectors.
func isInvisible(r rune) bool {
	switch {
	case r >= 0xE0000 && r <= 0xE007F, r >= 0xE0100 && r <= 0xE01EF:
		return true
	case r >= 0xFE00 && r <= 0xFE0F, r >= 0x2061 && r <= 0x2064:
		return true
	case r == 0x200B, r == 0x200C, r == 0x200D, r == 0x2060, r == 0xFEFF:
		return true
	}
	return false
}

// Lines is how many lines text has once its trailing newlines are dropped:
// 0 for none. It is the lines attribute of a paste block and the L of the
// page's "[paste N · L lines]" placeholder, so both count the same way.
// Pass sanitized text.
func Lines(text string) int {
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return 0
	}
	return strings.Count(text, "\n") + 1
}

// IsBinary reports text that is more than 1% NUL or U+FFFD (counting each
// invalid UTF-8 byte as one), which a paste refuses as binary. Run it on the
// raw text, before Sanitize turns invalid bytes into U+FFFD.
func IsBinary(s string) bool {
	total, bad := 0, 0
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		total++
		if r == 0 || r == utf8.RuneError {
			bad++
		}
		i += size
	}
	return bad*100 > total
}
