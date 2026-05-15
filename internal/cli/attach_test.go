package cli

import "testing"

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
