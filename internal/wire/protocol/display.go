package protocol

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Rune caps the daemon applies when it ingests free-text session fields.
// Clients may truncate further for layout.
const (
	// MaxTitleRunes caps a session title. Clients send the task prompt as
	// the title, so this keeps its first line readable on a card.
	MaxTitleRunes = 256
	// MaxFleetRunes caps a fleet label.
	MaxFleetRunes = 64
	// MaxDescriptionRunes caps the AI activity summary. The summarizer
	// already clamps to 60 runes; this bounds any other writer.
	MaxDescriptionRunes = 200
)

// C1 control runes that open escape sequences when a terminal decodes them.
const (
	runeDCS = '\u0090'
	runeSOS = '\u0098'
	runeST  = '\u009c'
	runeCSI = '\u009b'
	runeOSC = '\u009d'
	runePM  = '\u009e'
	runeAPC = '\u009f'
)

// SanitizeDisplayText makes untrusted text (agent or model output, client
// supplied labels) safe to print as one line in a terminal. It:
//
//   - removes ANSI escape sequences whole: CSI (ESC [ or U+009B), OSC, DCS,
//     SOS, PM and APC strings (ESC ] P X ^ _ or their C1 forms, through BEL
//     or ST, or to the end of s when unterminated) and two-byte escapes;
//   - drops every other C0 and C1 control, DEL and bidi override, and turns
//     a tab into a space;
//   - cuts at the first line break (LF, CR, VT, FF, NEL, U+2028, U+2029)
//     that follows visible text, so leading blank lines are skipped;
//   - drops invalid UTF-8, trims surrounding whitespace and keeps at most
//     maxRunes runes (maxRunes <= 0 means no cap).
//
// The result is idempotent: sanitizing it again returns it unchanged.
func SanitizeDisplayText(s string, maxRunes int) string {
	var b strings.Builder
	b.Grow(len(s))
	hasText := false
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		next := i + size
		switch {
		case r == utf8.RuneError && size <= 1:
			// Invalid UTF-8 (a lone 8-bit C1 byte lands here too).
		case r == 0x1b:
			next = skipEscape(s, next)
		case r == runeCSI:
			next = skipCSI(s, next)
		case r == runeOSC || r == runeDCS || r == runeSOS || r == runePM || r == runeAPC:
			next = skipControlString(s, next)
		case isLineBreak(r):
			if hasText {
				return finishDisplayText(b.String(), maxRunes)
			}
		case r == '\t':
			b.WriteByte(' ')
		case unicode.IsControl(r) || isBidiControl(r):
			// C0, DEL, the remaining C1 controls, bidi overrides.
		default:
			b.WriteRune(r)
			hasText = hasText || !unicode.IsSpace(r)
		}
		i = next
	}
	return finishDisplayText(b.String(), maxRunes)
}

func finishDisplayText(s string, maxRunes int) string {
	s = strings.TrimSpace(s)
	if maxRunes <= 0 || utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	n := 0
	for i := range s {
		if n == maxRunes {
			return strings.TrimRightFunc(s[:i], unicode.IsSpace)
		}
		n++
	}
	return s
}

func isLineBreak(r rune) bool {
	switch r {
	case '\n', '\r', '\v', '\f', '\u0085', '\u2028', '\u2029':
		return true
	}
	return false
}

// skipEscape returns the index just past the escape sequence whose ESC ended
// at i. A lone ESC (end of input, or followed by a byte that cannot continue
// a sequence) is dropped and scanning resumes at i.
func skipEscape(s string, i int) int {
	if i >= len(s) {
		return i
	}
	switch c := s[i]; {
	case c == '[':
		return skipCSI(s, i+1)
	case c == ']' || c == 'P' || c == 'X' || c == '^' || c == '_':
		return skipControlString(s, i+1)
	case c >= 0x20 && c <= 0x2f:
		// Intermediate bytes (e.g. ESC ( B), then one final byte.
		for i < len(s) && s[i] >= 0x20 && s[i] <= 0x2f {
			i++
		}
		if i < len(s) && s[i] >= 0x30 && s[i] <= 0x7e {
			i++
		}
		return i
	case c >= 0x30 && c <= 0x7e:
		return i + 1 // two-byte escape such as ESC 7 or ESC c
	default:
		return i
	}
}

// skipCSI returns the index just past a control sequence whose introducer
// ended at i: parameter bytes, intermediate bytes, one final byte. A byte
// outside those ranges aborts the sequence and is scanned normally.
func skipCSI(s string, i int) int {
	for i < len(s) && s[i] >= 0x30 && s[i] <= 0x3f {
		i++
	}
	for i < len(s) && s[i] >= 0x20 && s[i] <= 0x2f {
		i++
	}
	if i < len(s) && s[i] >= 0x40 && s[i] <= 0x7e {
		i++
	}
	return i
}

// skipControlString returns the index just past an OSC/DCS/SOS/PM/APC string
// whose introducer ended at i. It ends at BEL, at ST (ESC \ or U+009C), or
// just before any other ESC (which aborts the string and starts a new
// sequence). An unterminated string runs to the end of s.
func skipControlString(s string, i int) int {
	for i < len(s) {
		switch {
		case s[i] == 0x07:
			return i + 1
		case s[i] == 0x1b:
			if i+1 < len(s) && s[i+1] == '\\' {
				return i + 2
			}
			return i
		case strings.HasPrefix(s[i:], string(runeST)):
			return i + utf8.RuneLen(runeST)
		}
		i++
	}
	return i
}
