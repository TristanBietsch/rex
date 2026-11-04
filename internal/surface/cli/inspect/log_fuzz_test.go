package inspect

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

// stripFuzzSeeds are shared by both fuzz targets.
var stripFuzzSeeds = []string{
	"plain text\n",
	"\x1b[31mred\x1b[0m\n",
	"\x1b[2J\x1b[H• Test received.\x1b[5;1H\x1b[K",
	"\x1b[?25l\x1b[?25h",
	"\x1b]0;title\x07after",
	"\x1b]8;;https://example.com\x1b\\link\x1b]8;;\x1b\\",
	"\x1bP1$r0m\x1b\\", "\x1b_Gf=100;AAAA\x1b\\", "\x1b(B\x1b7\x1b8",
	"\x1b", "\x1b[", "\x1b[31", "trailing\x1b",
	"\xff\xfe broken utf8",
	// F-02 crasher: a kept lone ESC fused with the '[' left after the CSI
	// was removed, emitting "\x1b[".
	"\x1b\x1b[A[",
}

// FuzzStripANSI checks the transcript cleaner `rex log` prints through. It
// must not panic on arbitrary PTY bytes, must only ever remove bytes, must
// leave escape-free text untouched, and must never hand the terminal an
// ESC, so no escape sequence of any kind survives.
func FuzzStripANSI(f *testing.F) {
	for _, s := range stripFuzzSeeds {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, in []byte) {
		out := stripANSI(in)
		require.LessOrEqual(t, len(out), len(in), "stripANSI grew its input")
		if !bytes.Contains(in, []byte{0x1b}) {
			require.Equal(t, in, out, "escape-free input changed")
		}
		require.NotContains(t, string(out), "\x1b", "ESC in output %q (input %q)", out, in)
	})
}

// FuzzStripWriterChunked checks that `rex log -f`, which strips the
// transcript in whatever chunks it reads, prints exactly what stripping the
// whole transcript at once prints, wherever the chunk boundary falls.
func FuzzStripWriterChunked(f *testing.F) {
	for _, s := range stripFuzzSeeds {
		f.Add([]byte(s), uint(len(s)/2))
	}

	f.Fuzz(func(t *testing.T, in []byte, cut uint) {
		at := int(cut % uint(len(in)+1)) //nolint:gosec // G115: the modulo keeps the value within len(in).
		var out bytes.Buffer
		w := newStripWriter(&out)
		_, err := w.Write(in[:at])
		require.NoError(t, err)
		_, err = w.Write(in[at:])
		require.NoError(t, err)
		require.Equal(t, string(stripANSI(in)), out.String(), "split at %d of %q", at, in)
	})
}
