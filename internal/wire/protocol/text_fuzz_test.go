package protocol

import (
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

// FuzzValidateSlug checks ValidateSlug's documented promise: an accepted slug
// is one selector token (no whitespace) and carries no control or bidi runes
// that could inject terminal escapes.
func FuzzValidateSlug(f *testing.F) {
	for _, s := range []string{
		"alpha", "cc.opus.fix-auth-bug", "fix-bug-2", "ünïcode",
		"", " ", "a b", "a\tb", "a\nb", "a\u00a0b", "a\x1b[31m", "a\u202eb", "a\xffb",
	} {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, s string) {
		if ValidateSlug(s) != nil {
			return
		}
		require.NotEmpty(t, s)
		require.True(t, utf8.ValidString(s), "accepted invalid UTF-8 %q", s)
		require.Equal(t, []string{s}, strings.Fields(s), "accepted slug %q is not a single token", s)
		for _, r := range s {
			require.False(t, unicode.IsControl(r), "accepted control rune %U in %q", r, s)
			require.False(t, isBidiControl(r), "accepted bidi control %U in %q", r, s)
		}
	})
}

// FuzzSanitizeDisplayText checks SanitizeDisplayText's documented promise:
// the result is valid UTF-8, one trimmed line, free of control, C1 and bidi
// runes (so no escape sequence survives), within the rune cap, never longer
// than the input, and sanitizing it again changes nothing.
func FuzzSanitizeDisplayText(f *testing.F) {
	for _, s := range []string{
		"plain text", "  padded  ", "line1\nline2", "\n\nlate start", "a\rb",
		"\x1b[31mred\x1b[0m", "\x1b]0;title\x07rest", "\x1b]8;;http://x\x1b\\link\x1b]8;;\x1b\\",
		"\x1bPq\x1b\\", "\x1b", "a\x1b\x1b[A[", "\u009b31m", "\u009d0;t\u009c", "a\u0085b",
		"a\u2028b", "abc\u202edef", "a\xffb", "\x9b31m", "日本語 🐛", "a\tb",
	} {
		f.Add(s, 0)
		f.Add(s, 4)
	}

	f.Fuzz(func(t *testing.T, s string, maxRunes int) {
		maxRunes %= 512
		out := SanitizeDisplayText(s, maxRunes)
		require.True(t, utf8.ValidString(out), "invalid UTF-8 in %q", out)
		require.Equal(t, strings.TrimSpace(out), out, "untrimmed result %q", out)
		require.LessOrEqual(t, len(out), len(s), "result longer than input")
		if maxRunes > 0 {
			require.LessOrEqual(t, utf8.RuneCountInString(out), maxRunes)
		}
		for _, r := range out {
			require.False(t, unicode.IsControl(r), "control rune %U kept in %q", r, out)
			require.False(t, isBidiControl(r), "bidi control %U kept in %q", r, out)
			require.False(t, r == '\u2028' || r == '\u2029', "line separator %U kept in %q", r, out)
		}
		require.Equal(t, out, SanitizeDisplayText(out, maxRunes), "not idempotent")
	})
}

// FuzzNormalizeSessionName checks NormalizeSessionName's documented promise:
// the result is a single trimmed line with whitespace runs collapsed to one
// space, no control or bidi runes, valid UTF-8, at most MaxSessionNameRunes,
// and normalizing it again changes nothing.
func FuzzNormalizeSessionName(f *testing.F) {
	for _, s := range []string{
		"  hello  ", "a   b\t\tc", "line1\nline2\r\nline3", "red\x1b[31mtext",
		"a\u009bb", "abc\u202edef\u2066", "a\u2028b", "a\xffb", "   ",
		"日本語 タスク", "fix 🐛 bug",
		strings.Repeat("界", MaxSessionNameRunes),
		strings.Repeat("界", MaxSessionNameRunes) + "x",
		strings.Repeat("a ", 32) + "    ",
	} {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, s string) {
		out, err := NormalizeSessionName(s)
		if err != nil {
			require.ErrorIs(t, err, ErrSessionNameTooLong)
			require.Empty(t, out)
			return
		}
		require.True(t, utf8.ValidString(out), "invalid UTF-8 in %q", out)
		require.LessOrEqual(t, utf8.RuneCountInString(out), MaxSessionNameRunes)
		require.Equal(t, strings.TrimSpace(out), out, "untrimmed result %q", out)
		require.NotContains(t, out, "  ", "whitespace run survived in %q", out)
		for _, r := range out {
			if unicode.IsSpace(r) {
				require.Equal(t, ' ', r, "whitespace %U not collapsed to a space in %q", r, out)
			}
			require.False(t, unicode.IsControl(r), "control rune %U kept in %q", r, out)
			require.False(t, isBidiControl(r), "bidi control %U kept in %q", r, out)
		}

		again, err := NormalizeSessionName(out)
		require.NoError(t, err)
		require.Equal(t, out, again, "not idempotent")
	})
}
