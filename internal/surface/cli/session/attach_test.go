package session

import (
	"strings"
	"testing"
)

func TestPumpInput(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		readOnly bool
		sent     string
		detached bool
	}{
		{"forwards then detaches", "ab\x1dcd", false, "ab", true},
		{"eof detaches", "abc", false, "abc", true},
		// Read-only must still see Ctrl+] (raw mode traps the terminal
		// otherwise) but forward nothing.
		{"read-only detaches on key", "ab\x1dcd", true, "", true},
		{"read-only key first", "\x1d", true, "", true},
		{"read-only eof stays attached", "abc", true, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var sent strings.Builder
			detached := false
			pumpInput(strings.NewReader(c.in), c.readOnly,
				func(b []byte) { sent.Write(b) },
				func() { detached = true })
			if sent.String() != c.sent {
				t.Fatalf("sent %q, want %q", sent.String(), c.sent)
			}
			if detached != c.detached {
				t.Fatalf("detached=%v, want %v", detached, c.detached)
			}
		})
	}
}

func TestIndexByte(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		c    byte
		want int
	}{
		{"empty", nil, 0x1d, -1},
		{"absent", []byte("hello"), 0x1d, -1},
		{"first", []byte{0x1d, 'a'}, 0x1d, 0},
		{"middle", []byte("ab\x1dcd"), 0x1d, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := indexByte(c.in, c.c)
			if got != c.want {
				t.Fatalf("want %d got %d", c.want, got)
			}
		})
	}
}
