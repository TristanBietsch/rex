package protocol

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxSessionNameRunes caps a session display name. Counted in runes, not
// bytes or cells, so CJK and emoji names get the same budget.
const MaxSessionNameRunes = 64

// ErrSessionNameTooLong is returned by NormalizeSessionName for names over
// MaxSessionNameRunes after normalization.
var ErrSessionNameTooLong = errors.New("name exceeds 64 characters")

// NormalizeSessionName turns user input into a safe single-line display name:
// tabs/newlines become spaces, control characters (C0, C1 incl. ESC/CSI) and
// bidi overrides are dropped so a name can't inject terminal escapes or
// visually spoof the board, whitespace runs collapse, ends are trimmed.
// Invalid UTF-8 is dropped. "" is a valid result (it means "clear").
func NormalizeSessionName(s string) (string, error) {
	var b strings.Builder
	b.Grow(len(s))
	space := false
	for len(s) > 0 {
		r, size := utf8.DecodeRuneInString(s)
		s = s[size:]
		if r == utf8.RuneError && size <= 1 {
			continue
		}
		if r == '\t' || r == '\n' || r == '\r' || unicode.IsSpace(r) {
			space = true
			continue
		}
		if unicode.IsControl(r) || IsBidiControl(r) || r == '\u2028' || r == '\u2029' {
			continue
		}
		if space && b.Len() > 0 {
			b.WriteByte(' ')
		}
		space = false
		b.WriteRune(r)
	}
	out := b.String()
	if utf8.RuneCountInString(out) > MaxSessionNameRunes {
		return "", ErrSessionNameTooLong
	}
	return out, nil
}

// IsBidiControl reports whether r is a bidirectional formatting control:
// the embeddings and overrides U+202A–U+202E, the isolates U+2066–U+2069,
// and the marks U+200E, U+200F and U+061C. Text that carries them can be
// shown in an order other than the one it is stored in, so places that
// print untrusted text to a terminal drop them.
func IsBidiControl(r rune) bool {
	return (r >= '\u202a' && r <= '\u202e') || (r >= '\u2066' && r <= '\u2069') ||
		r == '\u200e' || r == '\u200f' || r == '\u061c'
}

// SessionNameKey is the comparison key of a display name: the name after
// NormalizeSessionName (a name too long to normalize is only trimmed), with
// format runes (Unicode category Cf: zero-width space, ZWJ, soft hyphen, \u2026)
// dropped and every rune mapped to the smallest rune of its unicode.SimpleFold
// orbit. Two names are the same when their keys are equal: Unicode simple
// case folding, as strings.EqualFold, with no NFC/NFKC normalization, no full
// folding (\u00df is not ss) and no locale rules. "" means no name.
func SessionNameKey(s string) string {
	n, err := NormalizeSessionName(s)
	if err != nil {
		n = strings.TrimSpace(s)
	}
	var b strings.Builder
	b.Grow(len(n))
	for _, r := range n {
		if unicode.Is(unicode.Cf, r) {
			continue
		}
		b.WriteRune(foldRune(r))
	}
	return strings.TrimSpace(b.String())
}

// SameSessionName reports whether two display names collide: both have a
// non-empty SessionNameKey and the keys are equal. Names with no key (empty,
// or only format runes) never collide.
func SameSessionName(a, b string) bool {
	ka := SessionNameKey(a)
	return ka != "" && ka == SessionNameKey(b)
}

// foldRune maps r to the smallest rune of its simple case-folding orbit, so
// every rune that strings.EqualFold treats as equal maps to the same rune.
func foldRune(r rune) rune {
	m := r
	for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
		if f < m {
			m = f
		}
	}
	return m
}
