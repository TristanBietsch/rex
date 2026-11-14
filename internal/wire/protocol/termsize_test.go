package protocol

import "testing"

func TestClampTermSize(t *testing.T) {
	tests := []struct {
		name         string
		c, r, wc, wr uint16
	}{
		{"zero is default", 0, 0, 0, 0},
		{"zero cols only", 0, 30, 0, 30},
		{"in range", 120, 32, 120, 32},
		{"bounds exact", 20, 5, 20, 5},
		{"upper exact", 1000, 500, 1000, 500},
		{"too small", 1, 1, 20, 5},
		{"too big", 1001, 501, 1000, 500},
		{"max uint16", 65535, 65535, 1000, 500},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, r := ClampTermSize(tc.c, tc.r)
			if c != tc.wc || r != tc.wr {
				t.Fatalf("ClampTermSize(%d,%d) = %d,%d; want %d,%d", tc.c, tc.r, c, r, tc.wc, tc.wr)
			}
		})
	}
}
