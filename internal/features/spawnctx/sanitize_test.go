package spawnctx

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSanitize(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"plain", "hello world", "hello world"},
		{"crlf", "a\r\nb", "a\nb"},
		{"lone cr", "a\rb\r", "a\nb\n"},
		{"tab kept", "a\tb", "a\tb"},
		{"sgr dropped", "\x1b[31mred\x1b[0m", "red"},
		{"osc dropped", "a\x1b]0;title\x07b", "ab"},
		{"c0 dropped", "a\x00b\x07c\x08d\x7f", "abcd"},
		{"c1 dropped", "a\u0085b\u009bc", "abc"},
		{"rlo dropped", "admin\u202egpj.exe", "admingpj.exe"},
		{"isolates dropped", "\u2066a\u2067b\u2068c\u2069", "abc"},
		{"embeddings dropped", "\u202aa\u202bb\u202cc\u202dd", "abcd"},
		{"marks dropped", "a\u200eb\u200fc\u061cd", "abcd"},
		{"invalid utf8", "a\xffb", "a\ufffdb"},
		{"cjk kept", "界 ✓", "界 ✓"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, Sanitize(tc.in))
		})
	}
}

func TestIsBinary(t *testing.T) {
	require.False(t, IsBinary(""))
	require.False(t, IsBinary("plain text\nwith lines"))
	require.True(t, IsBinary("\x00\x01\x02ELF"))
	require.True(t, IsBinary("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"))
	// exactly 1% is still text; just over is binary
	require.False(t, IsBinary("\x00"+strings.Repeat("a", 99)))
	require.True(t, IsBinary("\x00\x00"+strings.Repeat("a", 98)))
	require.True(t, IsBinary("\ufffd\ufffd"+strings.Repeat("a", 98)))
	require.True(t, IsBinary("\xff\xfe"+strings.Repeat("a", 98)))
}
