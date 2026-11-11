package export

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func texts(fs []Frame) []string {
	out := make([]string, len(fs))
	for i, f := range fs {
		out[i] = f.Text
	}
	return out
}

func frames(t *testing.T, in string, cuts ...int64) []Frame {
	t.Helper()
	fs, err := Frames(strings.NewReader(in), 40, 6, cuts)
	require.NoError(t, err)
	return fs
}

func TestFrames_endFrameOnly(t *testing.T) {
	fs := frames(t, "hello\r\nworld\r\n")
	require.Equal(t, []string{"hello\nworld"}, texts(fs))
	require.EqualValues(t, 14, fs[0].At)
}

func TestFrames_redrawHeavyCollapsesToDistinctFrames(t *testing.T) {
	var b strings.Builder
	// The same spinner screen repainted many times, then a changed screen.
	for range 50 {
		b.WriteString("\x1b[2J\x1b[Hthinking...\r\n> ask\r\n")
	}
	b.WriteString("\x1b[2J\x1b[Hdone: 42\r\n> ask\r\n")
	for range 20 {
		b.WriteString("\x1b[2J\x1b[Hdone: 42\r\n> ask\r\n")
	}
	got := texts(frames(t, b.String()))
	require.Equal(t, []string{"thinking...\n> ask", "done: 42\n> ask"}, got)
}

func TestFrames_cutsAtEveryClearScreen(t *testing.T) {
	in := "one\x1b[2J\x1b[Htwo\x1b[3J\x1b[Hthree"
	require.Equal(t, []string{"one", "two", "three"}, texts(frames(t, in)))
}

func TestFrames_altScreenEnterExit(t *testing.T) {
	in := "shell prompt\r\n\x1b[?1049h\x1b[Hfull screen ui\x1b[2;1Hrow two\x1b[?1049l$ back\r\n"
	got := texts(frames(t, in))
	require.Equal(t, []string{
		"shell prompt",
		"full screen ui\nrow two",
		"shell prompt\n$ back",
	}, got)
}

func TestFrames_otherAltScreenModes(t *testing.T) {
	for _, mode := range []string{"47", "1047"} {
		in := "main\x1b[?" + mode + "h\x1b[2J\x1b[Halt\x1b[?" + mode + "l"
		require.Equal(t, []string{"main", "alt", "main"}, texts(frames(t, in)), mode)
	}
}

func TestFrames_crOverwriteProgressBarCollapses(t *testing.T) {
	in := "download\r\n10%\r 50%\r100% done\r\n"
	got := texts(frames(t, in))
	require.Equal(t, []string{"download\n100% done"}, got)
}

func TestFrames_scrolledOffLinesAreKept(t *testing.T) {
	var b strings.Builder
	for i := range 12 {
		b.WriteString("line ")
		b.WriteString(string(rune('a' + i)))
		b.WriteString("\r\n")
	}
	fs := frames(t, b.String())
	require.Len(t, fs, 1)
	lines := strings.Split(fs[0].Text, "\n")
	require.Equal(t, "line a", lines[0])
	require.Equal(t, "line l", lines[len(lines)-1])
	require.Len(t, lines, 12)
}

func TestFrames_invalidUTF8Replaced(t *testing.T) {
	fs := frames(t, "ok \xff\xfe bad \xc3 end")
	require.Equal(t, "ok � bad � end", fs[0].Text)
}

func TestFrames_invalidUTF8NextToEscape(t *testing.T) {
	fs := frames(t, "a\xc9\x1b[31mb")
	require.Equal(t, "a�b", fs[0].Text)
}

func TestFrames_operatingSystemCommandsProduceNoText(t *testing.T) {
	in := "a" +
		"\x1b]52;c;aGVsbG8=\x07" + // clipboard write
		"\x1b]0;window title\x07" + // title
		"\x1b]2;other title\x1b\\" + // title, ST terminated
		"\x1b]7501;rex-private-payload\x07" + // rex private OSC
		"\x1b]8;;https://example.com\x1b\\link\x1b]8;;\x1b\\" +
		"b"
	got := frames(t, in)[0].Text
	require.Equal(t, "alinkb", got)
	for _, leak := range []string{"aGVsbG8", "title", "7501", "payload", "example.com"} {
		require.NotContains(t, got, leak)
	}
}

func TestFrames_splitEscapeAcrossReads(t *testing.T) {
	in := "x\x1b[2Jy\x1b[?1049hz\xc3\xa9"
	want := texts(frames(t, in))
	fs, err := Frames(&oneByteReader{r: strings.NewReader(in)}, 40, 6, nil)
	require.NoError(t, err)
	require.Equal(t, want, texts(fs))
}

type oneByteReader struct{ r *strings.Reader }

func (o *oneByteReader) Read(p []byte) (int, error) { return o.r.Read(p[:min(1, len(p))]) }

func TestFrames_turnCutsByOffset(t *testing.T) {
	in := "turn one\r\nturn two\r\n"
	// Cut after the first line (10 bytes).
	fs := frames(t, in, 10)
	require.Equal(t, []string{"turn one", "turn one\nturn two"}, texts(fs))
	require.EqualValues(t, 10, fs[0].At)
}

func TestFrames_emptyInput(t *testing.T) {
	require.Empty(t, frames(t, ""))
	require.Empty(t, frames(t, "\x1b[2J\x1b[H   \r\n"))
}

func TestFrames_rejectsBadSize(t *testing.T) {
	_, err := Frames(strings.NewReader("x"), 0, 5, nil)
	require.Error(t, err)
	_, err = Frames(strings.NewReader("x"), 5, 0, nil)
	require.Error(t, err)
}

func TestFrames_trailingBlanksTrimmed(t *testing.T) {
	require.Equal(t, "a\n\nb", frames(t, "a   \r\n\r\nb   ")[0].Text)
}

func TestFrames_readError(t *testing.T) {
	_, err := Frames(errReader{}, 40, 6, nil)
	require.Error(t, err)
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, bytes.ErrTooLarge }

func TestStrip(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"plain", "hello\nworld\n", "hello\nworld"},
		{"crlf", "a\r\nb\r\n", "a\nb"},
		{"sgr", "\x1b[31mred\x1b[0m ok", "red ok"},
		{"progress bar", "10%\r 50%\r100% done\nnext\n", "100% done\nnext"},
		{"overwrite keeps tail", "abcdef\rXY\n", "XYcdef"},
		{"osc 52 title 7501", "a\x1b]52;c;aGk=\x07\x1b]0;t\x07\x1b]7501;x\x07b", "ab"},
		{"invalid utf8", "a\xffb", "a�b"},
		{"controls dropped", "a\x00\x07\x08b", "ab"},
		{"tabs kept", "a\tb", "a\tb"},
		{"trailing spaces", "a   \nb  \n\n\n", "a\nb"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Strip(strings.NewReader(c.in))
			require.NoError(t, err)
			require.Equal(t, c.want, got)
		})
	}
}

func TestStrip_readError(t *testing.T) {
	_, err := Strip(errReader{})
	require.Error(t, err)
}

func TestLineTool(t *testing.T) {
	require.True(t, LineTool("echo"))
	require.True(t, LineTool("ollama"))
	require.False(t, LineTool("claude"))
	require.False(t, LineTool("antigravity"))
	require.False(t, LineTool("custom-tool"))
}
