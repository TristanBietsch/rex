package inspect

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

// FuzzStripANSI checks the transcript cleaner `rex log` prints through. It
// must not panic on arbitrary PTY bytes, must only ever remove bytes, must
// leave escape-free text untouched, and must not hand the terminal a CSI
// introducer (ESC '[').
func FuzzStripANSI(f *testing.F) {
	for _, s := range []string{
		"plain text\n",
		"\x1b[31mred\x1b[0m\n",
		"\x1b[2J\x1b[H• Test received.\x1b[5;1H\x1b[K",
		"\x1b[?25l\x1b[?25h",
		"\x1b]0;title\x07after",
		"\x1b", "\x1b[", "\x1b[31", "trailing\x1b",
		"\xff\xfe broken utf8",
	} {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, in []byte) {
		out := stripANSI(in)
		require.LessOrEqual(t, len(out), len(in), "stripANSI grew its input")
		if !bytes.Contains(in, []byte{0x1b}) {
			require.Equal(t, in, out, "escape-free input changed")
		}
		// Known failure under -fuzz: "\x1b\x1b[A[" strips to "\x1b[". A
		// lone ESC is kept, the CSI after it is removed, and the bytes
		// either side join into a new introducer. Kept as a finding for the
		// hardening backlog.
		require.False(t, bytes.Contains(out, []byte("\x1b[")), "CSI introducer in output %q (input %q)", out, in)
	})
}
