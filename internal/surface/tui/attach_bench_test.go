package tui

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// Attach popup benchmarks (bench scenario S6). The popup re-renders on every
// output burst (up to ~33 fps) and on every spinner tick, so View cost while
// attached is what makes a busy agent feel choppy.

const (
	benchAttachCols = 211 // a full-screen terminal on a laptop display
	benchAttachRows = 57
)

// benchAttachModel returns a board of 10 sessions with the attach popup open
// at cols×rows, focused like a real attach.
func benchAttachModel(tb testing.TB, cols, rows int) Model {
	tb.Helper()
	// Render with colors, as in a real terminal; tests default to no color.
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	tb.Cleanup(func() { lipgloss.SetColorProfile(prev) })
	m := benchModel(10)
	// attachPopupInnerSize subtracts 14 columns and 12 rows of chrome.
	m.Width, m.Height = cols+14, rows+12
	st := &AttachState{SessionID: m.Sessions[0].ID, Slug: "bench", Cols: cols, Rows: rows}
	st.Term = newAttachTerm(st, cols, rows, 5000)
	m.Attach = st
	m.Focus = FocusAttach
	if gc, gr := attachPopupInnerSize(m.Width, m.Height, 0); gc != cols || gr != rows {
		tb.Fatalf("popup is %dx%d, want %dx%d", gc, gr, cols, rows)
	}
	return m
}

// claudeLikeFrame returns one Ink-style repaint like Claude Code's: hide the
// cursor, home, then rewrite every row with 256-color runs, erasing each line
// first and stepping down with CUD. Most rows repeat from frame to frame; the
// spinner row and the status row change, as they do in a working session.
func claudeLikeFrame(i, cols, rows int) []byte {
	spin := []string{"✻", "✽", "✶", "✳", "✢", "·"}
	var b bytes.Buffer
	b.WriteString("\x1b[?25l\x1b[H")
	for y := 0; y < rows; y++ {
		b.WriteString("\x1b[2K")
		switch y {
		case rows - 6:
			fmt.Fprintf(&b, "\x1b[38;5;174m%s\x1b[39m \x1b[38;5;246mThinking… (%ds · ↓ %d tokens)\x1b[39m", spin[i%len(spin)], i/30, i*7)
		case rows - 4, rows - 2:
			fmt.Fprintf(&b, "\x1b[38;5;244m%s\x1b[39m", repeatRune('─', cols))
		case rows - 3:
			b.WriteString("\x1b[1m❯\x1b[22m ")
		case rows - 1:
			fmt.Fprintf(&b, "  \x1b[38;5;211m⏵⏵ auto mode on\x1b[39m\x1b[38;5;246m (shift+tab to cycle) · %d\x1b[39m", i)
		default:
			// A transcript row: a bullet, a bold label and two colored runs.
			fmt.Fprintf(&b, "\x1b[38;5;%dm⏺\x1b[39m \x1b[1mUpdate\x1b[22m(internal/surface/tui/file_%02d.go) \x1b[38;5;114m+%d\x1b[39m \x1b[38;5;203m-%d\x1b[39m  \x1b[48;5;22m added line with a background color \x1b[49m",
				30+y%200, y, y*3, y)
		}
		if y < rows-1 {
			b.WriteString("\r\x1b[1B")
		}
	}
	b.WriteString("\x1b[?25h")
	return b.Bytes()
}

func repeatRune(r rune, n int) string {
	out := make([]rune, n)
	for i := range out {
		out[i] = r
	}
	return string(out)
}

// BenchmarkAttachView is the steady-state frame: the screen is full and
// unchanged, as on a spinner tick.
func BenchmarkAttachView(b *testing.B) {
	m := benchAttachModel(b, benchAttachCols, benchAttachRows)
	_, _ = m.Attach.Term.Write(claudeLikeFrame(0, benchAttachCols, benchAttachRows))
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = m.View()
	}
}

// benchAttachBannerModel is benchAttachModel with the banner row on and a
// description too long for it, so the marquee scrolls. The agent keeps the
// same cols×rows, so the frame is directly comparable.
func benchAttachBannerModel(tb testing.TB, cols, rows int) Model {
	tb.Helper()
	m := benchAttachModel(tb, cols, rows)
	m.Height++ // the banner row is one more row of chrome
	m.Sessions[0].Description = strings.Repeat("Rewriting the invoice rounding tests and the proration edge cases. ", 5)
	m.Sessions[0].CWD = "/work/code/rex"
	m.Attach.BannerRows = 1
	m.Attach.BannerSince = time.Unix(0, 0)
	if !bannerOverflows(m) {
		tb.Fatal("bench banner must overflow so it scrolls")
	}
	return m
}

// BenchmarkAttachView_BannerMarquee is BenchmarkAttachView with a scrolling
// banner whose window moves every frame. It must stay within about 10% of the
// no-banner frame.
func BenchmarkAttachView_BannerMarquee(b *testing.B) {
	m := benchAttachBannerModel(b, benchAttachCols, benchAttachRows)
	_, _ = m.Attach.Term.Write(claudeLikeFrame(0, benchAttachCols, benchAttachRows))
	var tick int
	m.Clock = func() time.Time { tick++; return time.Unix(0, 0).Add(bannerPause + time.Duration(tick)*bannerStep) }
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = m.View()
	}
}

// BenchmarkAttachFrame is one output burst: write a repaint, then render.
func BenchmarkAttachFrame(b *testing.B) {
	m := benchAttachModel(b, benchAttachCols, benchAttachRows)
	frames := make([][]byte, 64)
	for i := range frames {
		frames[i] = claudeLikeFrame(i, benchAttachCols, benchAttachRows)
	}
	b.ReportAllocs()
	b.ResetTimer()
	i := 0
	for b.Loop() {
		_, _ = m.Attach.Term.Write(frames[i%len(frames)])
		_ = m.View()
		i++
	}
}

// BenchmarkAttachReplay replays a real transcript.log (REX_BENCH_TRANSCRIPT)
// in 4 KiB chunks and renders after every 16 KiB, roughly what the popup's
// 30 ms coalescing window sees from a busy agent. Transcripts hold private
// session output, so none is committed; the benchmark skips without one.
func BenchmarkAttachReplay(b *testing.B) {
	path := os.Getenv("REX_BENCH_TRANSCRIPT")
	if path == "" {
		b.Skip("set REX_BENCH_TRANSCRIPT to a transcript.log to replay")
	}
	data, err := os.ReadFile(path) //nolint:gosec // G304: benchmark input chosen by the developer.
	if err != nil {
		b.Fatal(err)
	}
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for b.Loop() {
		m := benchAttachModel(b, benchAttachCols, benchAttachRows)
		since := 0
		for off := 0; off < len(data); off += 4096 {
			end := min(off+4096, len(data))
			_, _ = m.Attach.Term.Write(data[off:end])
			if since += end - off; since >= 16384 {
				_ = m.View()
				since = 0
			}
		}
	}
}
