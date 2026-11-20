//go:build linux

package server

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseStatPPID(t *testing.T) {
	cases := []struct {
		name, stat string
		want       int
		wantErr    bool
	}{
		{"plain", "42 (sleep) S 7 42 7 0 -1", 7, false},
		{"comm with spaces and parens", "42 (a) b (c d)) R 9 42 9", 9, false},
		{"no closing paren", "42 (sleep S 7", 0, true},
		{"too few fields", "42 (sleep) S", 0, true},
		{"ppid not a number", "42 (sleep) S x", 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseStatPPID(42, tc.stat)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}
