package protocol

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSanitizeDisplayText(t *testing.T) {
	cases := []struct {
		name string
		in   string
		max  int
		want string
	}{
		{"plain", "fix the auth bug", 0, "fix the auth bug"},
		{"trims", "  padded \t", 0, "padded"},
		{"tab becomes space", "a\tb", 0, "a b"},
		{"cuts at first newline", "first line\nsecond line", 0, "first line"},
		{"cuts at CR", "shown\roverwritten", 0, "shown"},
		{"cuts at CRLF", "one\r\ntwo", 0, "one"},
		{"cuts at line separator", "a\u2028b", 0, "a"},
		{"cuts at NEL", "a\u0085b", 0, "a"},
		{"leading blank lines skipped", "\n\n  real text\nmore", 0, "real text"},
		{"SGR stripped", "\x1b[1;31mred\x1b[0m text", 0, "red text"},
		{"cursor moves stripped", "a\x1b[2Ab\x1b[10;20Hc", 0, "abc"},
		{"private CSI stripped", "\x1b[?1049hscreen", 0, "screen"},
		{"OSC title BEL", "\x1b]0;pwned\x07title", 0, "title"},
		{"OSC 8 hyperlink ST", "\x1b]8;;http://evil\x1b\\click\x1b]8;;\x1b\\", 0, "click"},
		{"OSC 52 clipboard", "x\x1b]52;c;cm0gLXJmIC8=\x07y", 0, "xy"},
		{"unterminated OSC swallows rest", "keep\x1b]0;no terminator\nnext", 0, "keep"},
		{"DCS stripped", "a\x1bPq#0;2;0;0;0\x1b\\b", 0, "ab"},
		{"APC stripped", "a\x1b_payload\x1b\\b", 0, "ab"},
		{"two-byte escapes", "a\x1b7b\x1b8c\x1bcd", 0, "abcd"},
		{"charset designation", "a\x1b(Bb", 0, "ab"},
		{"lone trailing ESC", "abc\x1b", 0, "abc"},
		{"ESC before control", "a\x1b\x01b", 0, "ab"},
		{"C1 CSI rune", "a\u009b31mb", 0, "ab"},
		{"C1 OSC rune", "a\u009d0;t\u009cb", 0, "ab"},
		{"C1 OSC BEL", "a\u009d0;t\x07b", 0, "ab"},
		{"other C1 dropped", "a\u0084\u0090b", 0, "a"},
		{"C0 and DEL dropped", "a\x00b\x07c\x08d\x7fe", 0, "abcde"},
		{"bidi overrides dropped", "abc\u202edef\u2066", 0, "abcdef"},
		{"invalid UTF-8 dropped", "a\xffb\xc3", 0, "ab"},
		{"raw 8-bit CSI byte dropped", "a\x9b31mb", 0, "a31mb"},
		{"aborted CSI", "a\x1b[12\x01b", 0, "ab"},
		{"keeps unicode", "日本語 🐛 ünï", 0, "日本語 🐛 ünï"},
		{"caps runes", "abcdefgh", 4, "abcd"},
		{"caps by runes not bytes", "界界界界界", 2, "界界"},
		{"cap retrims", "abc defg", 4, "abc"},
		{"zero max means uncapped", strings.Repeat("a", 500), 0, strings.Repeat("a", 500)},
		{"only escapes", "\x1b[0m\x1b]0;x\x07", 0, ""},
		{"empty", "", 10, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, SanitizeDisplayText(tc.in, tc.max))
		})
	}
}

// FlattenDisplayText is the TUI's render-side sanitizer. These cases came
// with it from internal/surface/tui (FU-03). One changed meaning: the
// TUI's own sanitizer (ansi.Strip) dropped a lone C1 introducer rune and
// kept the bytes after it, and did not treat NEL as a line break. The
// shared scanner removes a C1 sequence whole, as it does an ESC one, and
// NEL is a line break, so it becomes a space.
func TestFlattenDisplayText(t *testing.T) {
	cases := map[string]struct{ in, want string }{
		"plain kept":             {"fix auth bug", "fix auth bug"},
		"unicode kept":           {"日本語 — ok ✓", "日本語 — ok ✓"},
		"newline to space":       {"a\nb\r\nc", "a b  c"},
		"tab to space":           {"a\tb", "a b"},
		"OSC 52 removed":         {"a\x1b]52;c;ZXZpbA==\x07b", "ab"},
		"OSC with ST":            {"a\x1b]0;title\x1b\\b", "ab"},
		"CSI removed":            {"a\x1b[2Jb\x1b[31mc", "abc"},
		"C0 and DEL dropped":     {"a\x00b\x07c\x7f", "abc"},
		"C1 sequence, NEL":       {"a\u009b31mb\u0085c", "ab c"},
		"other C1 dropped":       {"a\u0084b", "ab"},
		"invalid UTF-8":          {"a\xff\xfeb", "ab"},
		"line separators":        {"a\u2028b\u2029c", "a b c"},
		"bidi overrides dropped": {"abc\u202edef", "abcdef"},
		"not trimmed":            {"  padded  ", "  padded  "},
		"blank lines kept":       {"\n\nx", "  x"},
		"literal U+FFFD kept":    {"a�b", "a�b"},
		"empty":                  {"", ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.want, FlattenDisplayText(tc.in))
		})
	}
}

// The board renders every cell on every frame: clean text costs nothing.
func TestFlattenDisplayText_PlainDoesNotAllocate(t *testing.T) {
	s := "fix the auth bug — 日本語"
	require.Zero(t, testing.AllocsPerRun(100, func() { _ = FlattenDisplayText(s) }))
}
