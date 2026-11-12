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
		{"tag chars dropped", "a\U000E0001b\U000E0041\U000E007Fc", "abc"},
		{"zero width dropped", "a\u200bb\u200cc\u200dd\u2060e\ufefff", "abcdef"},
		{"variation selectors dropped", "a\ufe00b\ufe0fc\U000E0100d\U000E01EFe", "abcde"},
		{"emoji presentation selector dropped", "\u2764\ufe0f", "\u2764"},
		{"invisible operators dropped", "f\u2061x\u2062y\u2063z\u2064w", "fxyzw"},
		{"neighbours kept", "\ufdff\ufe10\U000E00FF\U000E01F0\u2060\u2065", "\ufdff\ufe10\U000E00FF\U000E01F0\u2065"},
		{"line separators kept", "a\u2028b\u2029c", "a\u2028b\u2029c"},
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

func TestLines(t *testing.T) {
	require.Equal(t, 0, Lines(""))
	require.Equal(t, 0, Lines("\n\n"))
	require.Equal(t, 1, Lines("a"))
	require.Equal(t, 1, Lines("a\n"))
	require.Equal(t, 3, Lines("a\n\nb\n"))
}
