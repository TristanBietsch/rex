package termfilter

import (
	"bytes"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// run filters in, written in two chunks split at cut, and flushes.
func run(t *testing.T, in []byte, cut int) []byte {
	t.Helper()
	var out bytes.Buffer
	w := NewWriter(&out)
	n, err := w.Write(in[:cut])
	require.NoError(t, err)
	require.Equal(t, cut, n)
	n, err = w.Write(in[cut:])
	require.NoError(t, err)
	require.Equal(t, len(in)-cut, n)
	require.NoError(t, w.Flush())
	return out.Bytes()
}

// FuzzFilter checks the Writer's promises: no panic; no string introducer
// (7-bit or C1), C1 rune, bidi control or invalid byte in the output; every
// ESC it lets through starts a sequence it allows; the result does not
// depend on where the input was split; and filtering twice changes nothing.
func FuzzFilter(f *testing.F) {
	for _, s := range []string{
		"a\tb\r\n", "\x1b[1;31mred\x1b[0m", "\x1b7x\x1b8", "\x1b(B", "é漢",
		"\x1b]52;c;aGk=\x07", "\x1b]8;;http://x\x1b\\", "\x1bPq#0\x1b\\",
		"\x1b_apc\x1b\\", "\x1b^pm\x1b\\", "\x1bXsos\x1b\\",
		"\u009d52;c;aGk=\u009c", "\u0090q\u009c", "\u202e", "\u2066", "\x07", "\u0085",
		"A\x9d52;c;aGk=\x9cB", "caf\x9e text", "\x1b]52;c;xx\x1b[31mTAIL",
		"\x1b]0;t\x18X", "\x1b%@\xc3\x9d", "\x1b[3\n1m", "\xe2\x80", "\xf0\x9f\x98\x80",
		"\x1b]0;\xc2\xc2\x9cX", "\x1b\x1b]\x1b\\]", "\xc2\x1b]",
	} {
		f.Add([]byte(s), uint(len(s)/2))
	}

	f.Fuzz(func(t *testing.T, in []byte, cut uint) {
		whole := run(t, in, len(in))
		split := run(t, in, int(cut%uint(len(in)+1)))
		require.Equal(t, whole, split, "split at %d of %q", cut%uint(len(in)+1), in)

		for _, bad := range []string{"\x1b]", "\x1bP", "\x1b_", "\x1b^", "\x1bX", "\u009d", "\u202e"} {
			require.NotContains(t, string(whole), bad, "input %q", in)
		}
		require.True(t, utf8.Valid(whole), "invalid UTF-8 in %q from %q", whole, in)
		for i, r := range string(whole) {
			require.False(t, r >= 0x80 && r <= 0x9f, "C1 rune %U at %d in %q from %q", r, i, whole, in)
			require.False(t, protocol.IsBidiControl(r), "bidi control %U at %d in %q from %q", r, i, whole, in)
			if r < 0x20 || r == 0x7f {
				require.Contains(t, "\t\n\r\b\x1b", string(r), "control %q at %d in %q from %q", r, i, whole, in)
			}
			if r == esc {
				require.Less(t, i+1, len(whole), "trailing ESC in %q from %q", whole, in)
				require.Contains(t, "[78=>DEM()*+-./", string(whole[i+1]),
					"ESC %q at %d in %q from %q", whole[i+1], i, whole, in)
			}
		}

		require.Equal(t, whole, run(t, whole, len(whole)), "filtering %q twice", in)
	})
}
