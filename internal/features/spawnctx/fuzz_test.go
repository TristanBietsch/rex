package spawnctx

import (
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

func FuzzSanitize(f *testing.F) {
	for _, s := range []string{
		"", "plain", "a\r\nb\rc", "\x1b[31mred\x1b[0m", "\x1b]8;;http://x\x1b\\link\x1b]8;;\x1b\\",
		"\u202eevil", "\u2066\u2069", "\xff\xfe", "\x1b[\xe2\x80\xae", "\u009b31m", "\x1b", "\x1bP\x00", "\r\x1b\n",
		"\U000E0041\U000E007F", "\u200b\u200d\ufeff\u2060",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		out := Sanitize(in)
		if !utf8.ValidString(out) {
			t.Fatalf("invalid UTF-8: %q -> %q", in, out)
		}
		for _, r := range out {
			if r == '\n' || r == '\t' {
				continue
			}
			if unicode.IsControl(r) || isBidi(r) || isInvisible(r) {
				t.Fatalf("unsafe rune %U in %q (from %q)", r, out, in)
			}
		}
		if strings.Contains(strings.ToLower(escapeClosers(out)), "</pasted") {
			t.Fatalf("closer survives escaping: %q", out)
		}
		if again := Sanitize(out); again != out {
			t.Fatalf("not idempotent: %q -> %q -> %q", in, out, again)
		}
	})
}
