package vt10x

import (
	"bytes"
	"testing"
)

func rowText(term Terminal, y, n int) string {
	var b []rune
	for x := 0; x < n; x++ {
		b = append(b, term.Cell(x, y).Char)
	}
	return string(b)
}

// A PTY read can end inside a rune; the rune must survive the split.
func TestWriteKeepsRuneSplitAcrossWrites(t *testing.T) {
	cases := []struct {
		name   string
		chunks []string
		want   string
	}{
		{"one split", []string{"ab\xe2\x94", "\x80cd"}, "ab─cd"},
		{"byte by byte", []string{"\xf0", "\x9f", "\x8e", "\x89x"}, "🎉x   "},
		{"split then escape", []string{"\xe2", "\x94\x80\x1b[31mr"}, "─r   "},
		{"split inside an OSC", []string{"\x1b]0;t\xc3", "\xa9\x07ok"}, "ok   "},
		{"invalid bytes skipped", []string{"a\xffb", "\x80c"}, "abc  "},
		{"incomplete then not a continuation", []string{"a\xe2", "b"}, "ab   "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			term := New(WithSize(5, 1))
			for _, c := range tc.chunks {
				n, err := term.Write([]byte(c))
				if err != nil || n != len(c) {
					t.Fatalf("Write(%q) = %d, %v; want %d, nil", c, n, err, len(c))
				}
			}
			if got := rowText(term, 0, 5); got != tc.want {
				t.Fatalf("row = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPartialRune(t *testing.T) {
	term := New(WithSize(5, 1))
	write(t, term, "a\xe2\x94")
	if got := term.PartialRune(); !bytes.Equal(got, []byte("\xe2\x94")) {
		t.Fatalf("PartialRune = %q, want the held bytes", got)
	}
	write(t, term, "\x80")
	if got := term.PartialRune(); got != nil {
		t.Fatalf("PartialRune = %q after the rune completed, want nil", got)
	}
	if c := term.Cell(1, 0).Char; c != '─' {
		t.Fatalf("cell = %q, want ─", c)
	}
}
