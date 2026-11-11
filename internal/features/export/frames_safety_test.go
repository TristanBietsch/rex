package export

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func noUnsafe(t *testing.T, s string) {
	t.Helper()
	for _, r := range s {
		require.False(t, unsafeRune(r), "unsafe rune %U in %q", r, s)
	}
}

func TestFrames_c1ControlsAndBidiAreDropped(t *testing.T) {
	in := "a\xc2\x9d52;c;QQ==\xc2\x9cb\xc2\x9b31mc‮def⁦g\x7f" + "\x1b(0qx\x1b(B\r\n"
	for _, strip := range []bool{false, true} {
		var got string
		if strip {
			s, err := Strip(strings.NewReader(in))
			require.NoError(t, err)
			got = s
		} else {
			got = frames(t, in)[0].Text
		}
		noUnsafe(t, got)
		require.NotContains(t, got, "‮")
		require.Contains(t, got, "def")
	}
}

func TestFrames_scrolledLinesAreSanitized(t *testing.T) {
	var b strings.Builder
	b.WriteString("top\xc2\x9d0;x\xc2\x9c‮line\r\n")
	for range 20 {
		b.WriteString("filler\r\n")
	}
	got := frames(t, b.String())[0].Text
	noUnsafe(t, got)
	require.Contains(t, got, "top0;xline")
}

func longTranscript() string {
	var b strings.Builder
	for i := range 5000 {
		fmt.Fprintf(&b, "line %d\r\n", i)
	}
	return b.String()
}

func TestFrames_noClearTranscriptStillGivesManyFrames(t *testing.T) {
	fs := frames(t, longTranscript())
	require.Greater(t, len(fs), 20)
	require.True(t, strings.HasSuffix(fs[len(fs)-1].Text, "line 4999"))
}

func TestFramesLimited_keepsTheLastFramesWithBoundedMemory(t *testing.T) {
	all, err := Frames(strings.NewReader(longTranscript()), 40, 6, nil)
	require.NoError(t, err)

	e, err := extract(strings.NewReader(longTranscript()), 40, 6, nil, 5)
	require.NoError(t, err)
	require.LessOrEqual(t, e.peak, 2*5+1, "frames held at once")
	require.Len(t, e.frames, 5)
	require.Equal(t, all[len(all)-5:], e.frames)
	require.Equal(t, len(all)-5, e.dropped)

	fs, dropped, err := FramesLimited(strings.NewReader(longTranscript()), 40, 6, nil, 5)
	require.NoError(t, err)
	require.Equal(t, e.frames, fs)
	require.Equal(t, e.dropped, dropped)

	_, _, err = FramesLimited(strings.NewReader("x"), 40, 6, nil, -1)
	require.Error(t, err)
}

func TestFrames_cutInsideARuneDoesNotSplitIt(t *testing.T) {
	fs := frames(t, "caf\xc3\xa9 x", 4)
	for _, f := range fs {
		require.NotContains(t, f.Text, "�")
	}
	require.Equal(t, "café x", fs[len(fs)-1].Text)
}

func TestStrip_lineEdits(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"erase line then shorter repaint", "long stale text\r\x1b[2Kshort\n", "short"},
		{"erase to end", "abcdef\r\x1b[3G\x1b[Kxy\n", "abxy"},
		{"cursor column spinner", "a\x1b[1Gb\x1b[1Gc\n", "c"},
		{"cursor column mid line", "abcdef\x1b[3GXY\n", "abXYef"},
		{"erase to start", "abcdef\x1b[3G\x1b[1K!\n", "  !def"},
		{"private use input is not a marker", "ab\U000f0005c\n", "abc"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Strip(strings.NewReader(c.in))
			require.NoError(t, err)
			require.Equal(t, c.want, got)
		})
	}
}
