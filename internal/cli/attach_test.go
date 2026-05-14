package cli

import "testing"

func TestContainsDetachSeq(t *testing.T) {
	cases := []struct {
		name string
		prev byte
		in   []byte
		want bool
	}{
		{"empty", 0, nil, false},
		{"no match", 0, []byte("hello"), false},
		{"split across boundary", 0x01, []byte("d"), true},
		{"within chunk", 0, []byte{0x01, 'd'}, true},
		{"within chunk surrounded", 0, []byte("abc\x01defg"), true},
		{"ctrl+a alone, no d", 0x01, []byte("e"), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := containsDetachSeq(c.in, c.prev)
			if got != c.want {
				t.Fatalf("want %v got %v", c.want, got)
			}
		})
	}
}
