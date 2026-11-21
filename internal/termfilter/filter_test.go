package termfilter

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// filter runs s through a fresh Writer in one Write and flushes it.
func filter(t *testing.T, s string) string {
	t.Helper()
	var out bytes.Buffer
	w := NewWriter(&out)
	n, err := w.Write([]byte(s))
	require.NoError(t, err)
	require.Equal(t, len(s), n)
	require.NoError(t, w.Flush())
	return out.String()
}

func TestFilter_Keeps(t *testing.T) {
	for _, s := range []string{
		"a\tb\r\n",
		"\x1b[1;31mred\x1b[0m",
		"\x1b[2J\x1b[H",
		"\x1b7x\x1b8",
		"\x1b(B",
		"é漢",
		"back\bspace",
		"\x1b=\x1b>",
		"\x1bD\x1bE\x1bM",
		"\x1b)0\x1b(%5",
		"\x1b[?1049h\x1b[38;2;1;2;3m\x1b[>4;1m\x1b[0 q",
		"\x0e\x1b)0qqq\x0f", // SO/SI line drawing (TERM=screen, tmux)
		"\u00dd\u00a0\u2028",
	} {
		require.Equal(t, s, filter(t, s), "input %q", s)
	}
}

func TestFilter_Drops(t *testing.T) {
	for _, seq := range []string{
		"\x1b]52;c;aGk=\x07",
		"\x1b]8;;http://x\x1b\\",
		"\x1b]0;title\x07",
		"\x1bPq#0\x1b\\",
		"\x1b_apc\x1b\\",
		"\x1b^pm\x1b\\",
		"\x1bXsos\x1b\\",
		"\u009d52;c;aGk=\u009c",
		"\u202e",
		"\u2066",
		"\x07",
		"\u0085",
	} {
		require.Equal(t, "AB", filter(t, "A"+seq+"B"), "sequence %q", seq)
	}
}

func TestFilter_DropsC1StringsAndControls(t *testing.T) {
	for _, seq := range []string{
		"\u0090q#0\u009c",    // DCS
		"\u009fapc\u009c",    // APC
		"\u009epm\u009c",     // PM
		"\u0098sos\u009c",    // SOS
		"\u009d0;t\x1b\\",    // C1 OSC ended by 7-bit ST
		"\x1b]0;t\u009c",     // 7-bit OSC ended by C1 ST
		"\u009b",             // C1 CSI
		"\u009c",             // stray C1 ST
		"\u200e\u200f\u061c", // bidi marks
		"\x00\x01\x1a\x1c\x7f",
	} {
		require.Equal(t, "AB", filter(t, "A"+seq+"B"), "sequence %q", seq)
	}
}

func TestFilter_DropsOtherShortEscapes(t *testing.T) {
	for _, seq := range []string{
		"\x1b%@",  // leave UTF-8 mode
		"\x1b%G",  // select UTF-8
		"\x1b F",  // 7-bit controls
		"\x1b G",  // 8-bit controls
		"\x1b#8",  // screen alignment test
		"\x1bc",   // full reset
		"\x1bk",   // screen/tmux title introducer
		"\x1b\\",  // stray ST
		"\x1bZ",   // identify terminal
		"\x1b$(B", // multi-byte charset designation
	} {
		require.Equal(t, "AB", filter(t, "A"+seq+"B"), "sequence %q", seq)
	}
}

func TestFilter_RawC1ByteIsNotAString(t *testing.T) {
	require.Equal(t, "A52;c;aGk=B", filter(t, "A\x9d52;c;aGk=\x9cB"))
	require.Equal(t, "caf text", filter(t, "caf\x9e text"))
}

func TestFilter_EscEndsString(t *testing.T) {
	require.Equal(t, "\x1b[31mTAIL", filter(t, "\x1b]52;c;xx"+"\x1b[31mTAIL"))
	require.Equal(t, "X", filter(t, "\x1b]0;t\x18X"))
	require.Equal(t, "X", filter(t, "\x1b]0;t\x1aX"))
	require.Equal(t, "\x1b7X", filter(t, "\x1b]0;t\x1b7X"))
}

func TestFilter_StringBodyIgnoresOtherBytes(t *testing.T) {
	// Newlines, C0 and UTF-8 (including a 0xC2 that is not U+009C) stay
	// inside the string; only the terminators end it.
	require.Equal(t, "X", filter(t, "\x1b]0;a\nb\x00\u00a0\xc2é\x9c\x1b\\X"))
	require.Equal(t, "X", filter(t, "\x1b]0;\xc2\x07X"))
	require.Equal(t, "X", filter(t, "\x1b]0;\xc2\xc2\x9cX"))
}

func TestFilter_UnterminatedStringIsBounded(t *testing.T) {
	in := "\x1b]" + strings.Repeat("a", maxStringBytes) + "bbbb"
	require.Equal(t, "bbbb", filter(t, in))

	// A body exactly at the cap still ends normally.
	in = "\x1b]" + strings.Repeat("a", maxStringBytes) + "\x07bbbb"
	require.Equal(t, "bbbb", filter(t, in))
}

func TestFilter_StringCapSplitNearBoundary(t *testing.T) {
	capAt := len("\x1b]") + maxStringBytes
	for _, in := range []string{
		"\x1b]" + strings.Repeat("a", maxStringBytes) + "bbbb",
		"\x1b]" + strings.Repeat("a", maxStringBytes) + "\x07bbbb",
	} {
		for i := capAt - 4; i <= capAt+4; i++ {
			require.Equal(t, "bbbb", filterSplit(t, in, i), "split at %d of %q…", i, in[:4])
		}
	}
}

// filterSplit runs s through a fresh Writer in two Writes split at i and
// flushes it.
func filterSplit(t *testing.T, s string, i int) string {
	t.Helper()
	var out bytes.Buffer
	w := NewWriter(&out)
	_, err := w.Write([]byte(s[:i]))
	require.NoError(t, err)
	_, err = w.Write([]byte(s[i:]))
	require.NoError(t, err)
	require.NoError(t, w.Flush())
	return out.String()
}

func TestFilter_StringAfterAbandonedCSI(t *testing.T) {
	for _, in := range []string{
		"\x1b[\x1b]52;c;aGk=\x07B",
		"\x1b[31\xc2\x9d52;c;x\xc2\x9cB", // UTF-8 U+009D … U+009C
	} {
		out := filter(t, in)
		require.NotContains(t, out, "]52;", "input %q", in)
		require.NotContains(t, out, "\xc2\x9d", "input %q", in)
		require.Equal(t, "B", out, "input %q", in)
	}
}

func TestFilter_CSI(t *testing.T) {
	// C0 controls inside a sequence act at once, as in a terminal.
	require.Equal(t, "\n\x1b[31mX", filter(t, "\x1b[3\n1mX"))
	require.Equal(t, "\x1b[31mX", filter(t, "\x1b[3\x00\x7f1mX"))
	// ESC restarts, CAN and SUB cancel, non-ASCII abandons a sequence.
	require.Equal(t, "\x1b[32mX", filter(t, "\x1b[31\x1b[32mX"))
	require.Equal(t, "1mX", filter(t, "\x1b[3\x181mX"))
	require.Equal(t, "éX", filter(t, "\x1b[3éX"))
	require.Equal(t, "éX", filter(t, "\x1béX"))
	require.Equal(t, "éX", filter(t, "\x1b(éX"))
	require.Equal(t, "X", filter(t, "\x1b(\x18X"))
	// A sequence longer than maxSeqBytes is dropped whole.
	long := "\x1b[" + strings.Repeat("1;", maxSeqBytes) + "mX"
	require.Equal(t, "X", filter(t, long))
	// Exactly maxSeqBytes is kept; one byte more is dropped.
	atCap := "\x1b[" + strings.Repeat("1", maxSeqBytes-3) + "m"
	require.Len(t, atCap, maxSeqBytes)
	require.Equal(t, atCap+"X", filter(t, atCap+"X"))
	overCap := "\x1b[" + strings.Repeat("1", maxSeqBytes-2) + "m"
	require.Equal(t, "X", filter(t, overCap+"X"))
}

func TestFilter_InvalidUTF8(t *testing.T) {
	const r = "\ufffd"
	for in, want := range map[string]string{
		"a\xffb":               "a" + r + "b",
		"a\xc0\xafb":           "a" + r + r + "b", // overlong: bad lead, stray continuation
		"a\xe6\xbcb":           "a" + r + "b",     // truncated rune is one U+FFFD
		"a\xe2\x80b":           "a" + r + "b",
		"a\xe0\x80b":           "a" + r + "b", // E0 needs A0–BF; 0x80 is then a raw C1 byte
		"a\xed\xa0\x80b":       "a" + r + r + "b",
		"a\xf4\x90b":           "a" + r + "b", // F4 needs 80–8F; 0x90 is then a raw C1 byte
		"a\xf0\x9f\x98\x80b":   "a\U0001f600b",
		"a\xf0\x9f\x98\x1b[mb": "a" + r + "\x1b[mb",
	} {
		require.Equal(t, want, filter(t, in), "input %q", in)
	}
}

func TestFilter_EverySplitPoint(t *testing.T) {
	in := "a\tb\r\n\x1b[1;31mred\x1b[0m é漢\U0001f600 \x1b]52;c;aGk=\x07x\x1b]8;;http://x\x1b\\y" +
		"\u009d0;t\u009cz\x1bPq#0\x1b\\\x1b7\x1b(B\x1b8\u202e\x9dcaf\x9e\x1b]0;t\x18w" +
		"\x1b]0;\xc2\xa0\xc2\x9cv\x1b%@\x1b[3\n1m\x1b]0;x\x1b[31mT\xe2\x80"
	want := filter(t, in)
	require.Equal(t, "a\tb\r\n\x1b[1;31mred\x1b[0m é漢\U0001f600 xyz\x1b7\x1b(B\x1b8cafwv\n\x1b[31m\x1b[31mT\ufffd", want)

	for i := 0; i <= len(in); i++ {
		var out bytes.Buffer
		w := NewWriter(&out)
		_, err := w.Write([]byte(in[:i]))
		require.NoError(t, err)
		_, err = w.Write([]byte(in[i:]))
		require.NoError(t, err)
		require.NoError(t, w.Flush())
		require.Equal(t, want, out.String(), "split at %d", i)
	}
}

func TestWriter_FlushEndsPendingState(t *testing.T) {
	var out bytes.Buffer
	w := NewWriter(&out)

	_, err := w.Write([]byte("A\x1b[31"))
	require.NoError(t, err)
	require.NoError(t, w.Flush())
	_, err = w.Write([]byte("mB\xe6\xbc"))
	require.NoError(t, err)
	require.Equal(t, "AmB", out.String(), "a pending CSI is dropped, not completed later")
	require.NoError(t, w.Flush())
	require.Equal(t, "AmB\ufffd", out.String(), "a pending partial rune becomes U+FFFD")
	require.NoError(t, w.Flush())
	require.Equal(t, "AmB\ufffd", out.String(), "a second Flush writes nothing")

	_, err = w.Write([]byte("\x1b]0;t"))
	require.NoError(t, err)
	require.NoError(t, w.Flush())
	_, err = w.Write([]byte("C"))
	require.NoError(t, err)
	require.Equal(t, "AmB\ufffdC", out.String())
}

var errWrite = errors.New("write failed")

type failWriter struct{ calls int }

func (f *failWriter) Write([]byte) (int, error) {
	f.calls++
	return 0, errWrite
}

func TestWriter_ReportsWriteErrors(t *testing.T) {
	fw := &failWriter{}
	w := NewWriter(fw)

	n, err := w.Write([]byte("\x1b]0;t\x07"))
	require.NoError(t, err, "nothing to write: the underlying writer is not called")
	require.Equal(t, 6, n)
	require.Zero(t, fw.calls)

	n, err = w.Write([]byte("text\xe6"))
	require.ErrorIs(t, err, errWrite)
	require.Zero(t, n)

	require.ErrorIs(t, w.Flush(), errWrite)
}

// halfWriter takes half of every buffer and reports no error, breaking
// the io.Writer contract the way a buggy writer can.
type halfWriter struct{}

func (halfWriter) Write(p []byte) (int, error) { return len(p) / 2, nil }

func TestWriter_ReportsShortWrites(t *testing.T) {
	w := NewWriter(halfWriter{})

	n, err := w.Write([]byte("hello\xe6"))
	require.ErrorIs(t, err, io.ErrShortWrite)
	require.Zero(t, n)

	require.ErrorIs(t, w.Flush(), io.ErrShortWrite)
}
