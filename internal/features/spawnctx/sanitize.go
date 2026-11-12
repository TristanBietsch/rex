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
//     the same way.
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
		if unicode.IsControl(r) || isBidi(r) {
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
