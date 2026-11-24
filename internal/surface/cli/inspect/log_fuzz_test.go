package inspect

import (
	"bytes"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// hasHidden reports whether b holds a control the stripper removes from
// otherwise valid text: a C0 control other than tab and newline, a C1 rune
// or a bidi control.
func hasHidden(b []byte) bool {
	for _, r := range string(b) {
		if (r < 0x20 && r != '\t' && r != '\n') || (r >= 0x80 && r <= 0x9f) || protocol.IsBidiControl(r) {
			return true
		}
	}
	return false
}

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
	"\xff\xfe broken utf8", "a\x07b\rc", "a\u009bb", "a\x9bb", "a\u202eb",
	"caf\u00e9 \u2713", "\xe2\x82", "\x1b[3\r1m",
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
		for _, b := range out {
			require.False(t, b < 0x20 && b != '\t' && b != '\n', "control byte %#x in output %q (input %q)", b, out, in)
		}
		require.True(t, utf8.Valid(out), "invalid UTF-8 in output %q (input %q)", out, in)
		for _, r := range string(out) {
			require.False(t, r >= 0x80 && r <= 0x9f, "C1 rune %U in output %q (input %q)", r, out, in)
			require.False(t, protocol.IsBidiControl(r), "bidi control %U in output %q (input %q)", r, out, in)
		}
		if !bytes.ContainsAny(in, "\x1b\x07\x08\r") && utf8.Valid(in) && !hasHidden(in) {
			require.Equal(t, in, out, "plain text changed")
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
		require.NoError(t, w.flush())
		require.Equal(t, string(stripANSI(in)), out.String(), "split at %d of %q", at, in)
	})
}
