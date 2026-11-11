package pty

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/hinshun/vt10x"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/testutil/uvfixture"
)

func screenLines(term vt10x.Terminal) []string {
	term.Lock()
	defer term.Unlock()
	cols, rows := term.Size()
	out := make([]string, rows)
	for y := range out {
		r := make([]rune, cols)
		for x := range r {
			r[x] = term.Cell(x, y).Char
			if r[x] == 0 {
				r[x] = ' '
			}
		}
		out[y] = string(r)
	}
	return out
}

// An ultraviolet agent drawing at the size the mirror already has.
func TestUVLogo_LiveStream(t *testing.T) {
	for _, size := range [][2]int{{120, 32}, {177, 44}} {
		cols, rows := size[0], size[1]
		mirror := newScreen(t, cols, rows, string(uvfixture.Frame(cols, rows)))
		require.Equal(t, uvfixture.Expected(cols, rows), screenLines(mirror), "%dx%d", cols, rows)
	}
}

// The daemon spawns agents at 120x32 and the first attach grows the PTY. The
// mirror is grown with it and the agent redraws at the new size. The mirror
// and a repaint of it into a fresh attach-side emulator must both show the
// logo centered, with the tab-positioned tag where a real terminal puts it.
func TestUVLogo_MirrorAfterGrowRepaints(t *testing.T) {
	for _, size := range [][2]int{{177, 44}, {240, 60}, {138, 36}} {
		cols, rows := size[0], size[1]
		mirror := newScreen(t, 120, 32)
		mirror.Resize(cols, rows)
		_, err := mirror.Write(uvfixture.Frame(cols, rows))
		require.NoError(t, err)

		want := uvfixture.Expected(cols, rows)
		require.Equal(t, want, screenLines(mirror), "mirror %dx%d", cols, rows)

		repainted, _ := repaintOf(t, mirror)
		require.Equal(t, want, screenLines(repainted), "repaint %dx%d", cols, rows)
		requireSameScreen(t, mirror, repainted, fmt.Sprintf("%dx%d", cols, rows))
	}
}

// Growing again and again (the user enlarges the terminal) keeps working.
func TestUVLogo_RepeatedGrowAndShrink(t *testing.T) {
	mirror := newScreen(t, 120, 32)
	for _, size := range [][2]int{{177, 44}, {90, 30}, {200, 50}, {121, 33}} {
		cols, rows := size[0], size[1]
		mirror.Resize(cols, rows)
		_, err := mirror.Write(uvfixture.Frame(cols, rows))
		require.NoError(t, err)
		require.Equal(t, uvfixture.Expected(cols, rows), screenLines(mirror), "%dx%d", cols, rows)
	}
}

var (
	reRep   = regexp.MustCompile("\x1b\\[\\d*b")
	reCHT   = regexp.MustCompile("\x1b\\[\\d*I")
	reDECST = regexp.MustCompile(regexp.QuoteMeta("\x1b[?5W"))
)

// TestU6CaptureReplay replays a raw agent capture (for example `rex log
// -bytes <session>` of an Antigravity run, kept outside the repository) into
// a mirror at the size the daemon spawns agents at, grows it to the size the
// attach popup used, and prints the screen before and after. It is skipped
// unless REX_U6_CAPTURE names the file; REX_U6_SIZE ("COLSxROWS", default
// 177x44) is the grown size. It asserts nothing: it is for looking.
func TestU6CaptureReplay(t *testing.T) {
	path := os.Getenv("REX_U6_CAPTURE")
	if path == "" {
		t.Skip("set REX_U6_CAPTURE to a raw capture file to replay it")
	}
	raw, err := os.ReadFile(path) //nolint:gosec // G703: a developer-supplied capture file, test-only
	require.NoError(t, err)

	cols, rows := 177, 44
	if v := os.Getenv("REX_U6_SIZE"); v != "" {
		c, r, ok := strings.Cut(v, "x")
		require.True(t, ok, "REX_U6_SIZE must be COLSxROWS")
		cols, err = strconv.Atoi(c)
		require.NoError(t, err)
		rows, err = strconv.Atoi(r)
		require.NoError(t, err)
	}

	t.Logf("capture: %d bytes; REP %d, CHT %d, HT %d, DECST8C %d",
		len(raw), len(reRep.FindAll(raw, -1)), len(reCHT.FindAll(raw, -1)),
		strings.Count(string(raw), "\t"), len(reDECST.FindAll(raw, -1)))

	mirror := newScreen(t, 120, 32, string(raw))
	t.Logf("at 120x32:\n%s", strings.Join(trimmedLines(mirror), "\n"))
	mirror.Resize(cols, rows)
	t.Logf("after growing to %dx%d (before the agent redraws):\n%s", cols, rows, strings.Join(trimmedLines(mirror), "\n"))
	repainted, _ := repaintOf(t, mirror)
	t.Logf("repaint of the grown mirror:\n%s", strings.Join(trimmedLines(repainted), "\n"))
	fmt.Fprintln(os.Stderr, "U6 capture replayed; see -v output above")
}

func trimmedLines(term vt10x.Terminal) []string {
	lines := screenLines(term)
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	return lines
}
