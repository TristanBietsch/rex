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
		if unicode.IsControl(r) || isBidiControl(r) || r == ' ' || r == ' ' {
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

func isBidiControl(r rune) bool {
	return (r >= '‪' && r <= '‮') || (r >= '⁦' && r <= '⁩') ||
		r == '‎' || r == '‏' || r == '؜'
}
