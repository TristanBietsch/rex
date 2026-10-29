package tui

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unsafe"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/hinshun/vt10x"
	"github.com/muesli/termenv"
)

// Local scrollback for the attach popup. Agents on the primary screen (codex
// inline, ollama, echo, Claude Code without fullscreen) print history that
// scrolls off the emulator's top edge; our vt10x fork hands each such line to
// a bounded ring here. Scroll mode (ctrl+pgup, or wheel up on the primary
// screen) shows it; keys drive the view instead of reaching the agent until
// it returns to live output. Alt-screen agents keep their own history and are
// untouched.

const (
	attachScrollbackDefault = 5000
	attachScrollbackMax     = 50000
	// attachScrollbackMaxBytes caps glyph memory regardless of the line
	// setting: 50k lines of a wide, colorful agent would otherwise be ~200 MB.
	attachScrollbackMaxBytes = 8 << 20
	// attachScrollEvictLogEvery rate-limits the "evicted" log line.
	attachScrollEvictLogEvery = 30 * time.Second
	// attachScrollWheelLines is how far one wheel notch moves the view.
	attachScrollWheelLines = 3
)

var (
	glyphBytes   = int(unsafe.Sizeof(vt10x.Glyph{}))
	lineOverhead = int(unsafe.Sizeof([]vt10x.Glyph(nil)))
)

// attachScrollback is a bounded FIFO of rows that scrolled off the top of the
// attach emulator. Line 0 is the oldest. Only the Bubble Tea goroutine
// touches it (vt10x calls push from Term.Write inside Update).
type attachScrollback struct {
	sessionID string
	lines     [][]vt10x.Glyph
	maxLines  int
	bytes     int

	captured, evicted int
	evictPending      int
	lastEvictLog      time.Time
}

func newAttachScrollback(sessionID string, maxLines int) *attachScrollback {
	return &attachScrollback{sessionID: sessionID, maxLines: maxLines}
}

func lineBytes(l []vt10x.Glyph) int { return len(l)*glyphBytes + lineOverhead }

// Len is the number of retained lines; nil-safe.
func (s *attachScrollback) Len() int {
	if s == nil {
		return 0
	}
	return len(s.lines)
}

// Line returns retained line i (0 = oldest).
func (s *attachScrollback) Line(i int) []vt10x.Glyph { return s.lines[i] }

// push appends a line, evicting the oldest past either cap.
func (s *attachScrollback) push(l []vt10x.Glyph) {
	if s == nil || s.maxLines <= 0 {
		return
	}
	s.lines = append(s.lines, l)
	s.bytes += lineBytes(l)
	s.captured++
	n := 0
	for len(s.lines) > s.maxLines || (s.bytes > attachScrollbackMaxBytes && len(s.lines) > 1) {
		s.bytes -= lineBytes(s.lines[0])
		s.lines[0] = nil
		s.lines = s.lines[1:]
		n++
	}
	if n == 0 {
		return
	}
	s.evicted += n
	s.evictPending += n
	if now := time.Now(); now.Sub(s.lastEvictLog) >= attachScrollEvictLogEvery {
		slog.Info("tui: attach scrollback evicted", "session", s.sessionID, "lines", s.evictPending,
			"retained", len(s.lines), "bytes", s.bytes)
		s.evictPending = 0
		s.lastEvictLog = now
	}
}

func attachScrollbackLimit(m Model) int {
	if m.Store == nil {
		return attachScrollbackDefault
	}
	v, ok := m.Store.Get("attach_scrollback_lines").(int)
	if !ok {
		return attachScrollbackDefault
	}
	return max(0, min(v, attachScrollbackMax))
}

// newAttachTerm builds the popup's emulator with scrollback capture wired to
// st (disabled when limit is 0).
func newAttachTerm(st *AttachState, cols, rows, limit int) vt10x.Terminal {
	opts := []vt10x.TerminalOption{vt10x.WithSize(cols, rows)}
	if limit > 0 {
		st.sb = newAttachScrollback(st.SessionID, limit)
		opts = append(opts, vt10x.WithScrollback(st.onScrollback))
	}
	return vt10x.New(opts...)
}

// onScrollback runs inside Term.Write with the emulator locked. While the
// user is scrolled up, bumping the offset keeps the view on the same text.
func (a *AttachState) onScrollback(l []vt10x.Glyph) {
	a.sb.push(l)
	if a.ScrollOff > 0 {
		a.ScrollOff = min(a.ScrollOff+1, a.sb.Len())
		a.scrollNew++
	}
}

// Scrolling reports whether scroll mode is active.
func (a *AttachState) Scrolling() bool { return a.ScrollOff > 0 }

// onPrimaryScreen reads the emulator mode; call without the term lock held.
func (a *AttachState) onPrimaryScreen() bool {
	a.Term.Lock()
	mode := a.Term.Mode()
	a.Term.Unlock()
	return mode&vt10x.ModeAltScreen == 0
}

// scrollAttach moves the view by delta lines (positive = back in history),
// entering scroll mode from live and leaving it on reaching the bottom.
func scrollAttach(a *AttachState, delta int, how string) {
	was := a.ScrollOff
	next := max(0, min(was+delta, a.sb.Len()))
	if was > 0 && next == 0 {
		exitAttachScroll(a, "bottom")
		return
	}
	a.ScrollOff = next
	if was == 0 && next > 0 {
		a.scrollNew = 0
		slog.Info("tui: attach scroll enter", "session", a.SessionID, "via", how, "offset", next, "lines", a.sb.Len())
	}
}

// exitAttachScroll returns the view to live output.
func exitAttachScroll(a *AttachState, reason string) {
	if a.ScrollOff == 0 {
		return
	}
	slog.Info("tui: attach scroll exit", "session", a.SessionID, "reason", reason, "new_lines", a.scrollNew)
	a.ScrollOff = 0
	a.scrollNew = 0
}

// afterAttachOutput leaves scroll mode when the agent switches to the alt
// screen: the history then belongs to a different screen.
func afterAttachOutput(a *AttachState) {
	if a.Scrolling() && !a.onPrimaryScreen() {
		exitAttachScroll(a, "alt-screen")
	}
}

// updateAttachScrollKey handles keys while scroll mode is active. Nothing
// reaches the agent; "::" still opens the command bar.
func updateAttachScrollKey(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	a := m.Attach
	// A held ':' never reaches the agent from scroll mode; 'q' keeps its
	// scroll meaning here, so only "::" is intercepted.
	m, cmd, handled, _ := interceptAttachHold(m, k, false)
	if handled {
		return m, cmd
	}
	page := max(1, a.Rows-1)
	half := max(1, a.Rows/2)
	switch k.Type {
	case tea.KeyUp, tea.KeyCtrlP:
		scrollAttach(a, 1, "key")
	case tea.KeyDown, tea.KeyCtrlN:
		scrollAttach(a, -1, "key")
	case tea.KeyPgUp, tea.KeyCtrlPgUp, tea.KeyCtrlB:
		scrollAttach(a, page, "key")
	case tea.KeyPgDown, tea.KeyCtrlPgDown, tea.KeyCtrlF:
		scrollAttach(a, -page, "key")
	case tea.KeyCtrlU:
		scrollAttach(a, half, "key")
	case tea.KeyCtrlD:
		scrollAttach(a, -half, "key")
	case tea.KeyHome:
		scrollAttach(a, a.sb.Len(), "key")
	case tea.KeyEnd, tea.KeyEsc:
		exitAttachScroll(a, "key")
	case tea.KeyRunes:
		if len(k.Runes) != 1 || k.Paste {
			return m, nil
		}
		switch k.Runes[0] {
		case 'k':
			scrollAttach(a, 1, "key")
		case 'j':
			scrollAttach(a, -1, "key")
		case 'g':
			scrollAttach(a, a.sb.Len(), "key")
		case 'G', 'q':
			exitAttachScroll(a, "key")
		}
	}
	return m, nil
}

// attachGlyphRow renders one row of cols cells as exactly cols terminal
// columns; cursorX < 0 draws no cursor. Callers reading live cells must hold
// the term lock.
//
// vt10x gives every rune one cell, but the terminal draws a wide (CJK,
// emoji) rune over two columns, so a row with wide runes came out wider than
// the popup, which then re-centered and threw off mouse hit-testing. A blank
// cell after a wide rune is taken as its right half (apps that place text by
// column leave it empty) and skipped; text written right after the rune
// shifts right instead, and the row is cut at cols.
func attachGlyphRow(cols int, cell func(x int) vt10x.Glyph, cursorX int) string {
	profile := lipgloss.ColorProfile()
	var row strings.Builder
	row.Grow(cols + 32)
	var key glyphKey
	open := false  // an SGR run is open and needs a reset
	width := 0     // columns drawn so far
	simple := true // every rune drawn is one column wide
	for x := 0; x < cols; x++ {
		g := cell(x)
		r := g.Char
		if r == 0 || r == ' ' {
			r = ' '
		}
		w := runeCols(r)
		if width+w > cols {
			break // a wide rune straddling the right edge
		}
		simple = simple && w == 1
		// Runs break on the raw cell attributes; the style is built once
		// per distinct key, not per cell.
		k := glyphKey{fg: g.FG, bg: g.BG, mode: g.Mode & glyphAttrs, cursor: x == cursorX}
		if x == 0 || k != key {
			if open {
				row.WriteString(sgrReset)
			}
			sgr := glyphSGR(profile, k)
			row.WriteString(sgr)
			open = sgr != ""
			key = k
		}
		row.WriteRune(r)
		width += w
		if w == 2 && x+1 < cols && x+1 != cursorX && isBlankGlyph(cell(x+1)) {
			x++
		}
	}
	if open {
		row.WriteString(sgrReset)
	}
	out := row.String()
	if !simple {
		// Per-rune widths can undercount a grapheme cluster split over
		// cells (emoji + variation selector): measure the real thing.
		if width = ansi.StringWidth(out); width > cols {
			out = ansi.Truncate(out, cols, "")
			width = ansi.StringWidth(out)
		}
	}
	return out + strings.Repeat(" ", cols-width)
}

// glyphKey is everything besides the rune that decides how a cell looks.
type glyphKey struct {
	fg, bg vt10x.Color
	mode   int16 // the glyphAttrs bits
	cursor bool
}

// glyphAttrs are the Glyph.Mode bits glyphStyle renders.
const glyphAttrs = vt10x.AttrReverse | vt10x.AttrBold | vt10x.AttrItalic | vt10x.AttrUnderline

const sgrReset = "\x1b[0m"

// sgrCache memoizes the SGR sequence that opens a run of each glyphKey for
// one color profile. Building a lipgloss style and color strings per cell
// made one colorful frame cost ~10 ms and 125k allocations.
var sgrCache struct {
	sync.Mutex
	profile termenv.Profile
	seqs    map[glyphKey]string
}

// sgrCacheMax bounds the cache (a 24-bit gradient has endless keys).
const sgrCacheMax = 4096

// glyphSGR returns the escape sequence that starts a run of cells with key
// k under profile p, "" when the run needs none. lipgloss renders a run as
// SGR + text + reset, so the sequence is what precedes the text.
func glyphSGR(p termenv.Profile, k glyphKey) string {
	sgrCache.Lock()
	defer sgrCache.Unlock()
	if sgrCache.seqs == nil || sgrCache.profile != p || len(sgrCache.seqs) >= sgrCacheMax {
		sgrCache.seqs = make(map[glyphKey]string)
		sgrCache.profile = p
	}
	if s, ok := sgrCache.seqs[k]; ok {
		return s
	}
	st := glyphStyle(vt10x.Glyph{FG: k.fg, BG: k.bg, Mode: k.mode})
	if k.cursor {
		// Block cursor: invert the cell, as a terminal does.
		st = st.Reverse(!st.GetReverse())
	}
	s, _, _ := strings.Cut(st.Render("x"), "x")
	sgrCache.seqs[k] = s
	return s
}

// runeCols is the number of terminal columns r takes: 1 below U+0300 (no
// wide or combining runes there), else per ansi.StringWidth.
func runeCols(r rune) int {
	if r < 0x300 {
		return 1
	}
	return ansi.StringWidth(string(r))
}

// isBlankGlyph reports whether g draws nothing: an empty or space cell on
// the default background, not reversed or underlined.
func isBlankGlyph(g vt10x.Glyph) bool {
	return (g.Char == 0 || g.Char == ' ') && g.BG == vt10x.DefaultBG &&
		g.Mode&(vt10x.AttrReverse|vt10x.AttrUnderline) == 0
}

var blankGlyph = vt10x.Glyph{Char: ' ', FG: vt10x.DefaultFG, BG: vt10x.DefaultBG}

// attachViewRow renders popup row y: the live screen, or while scrolled the
// slice of scrollback + live rows the offset selects. Scrollback rows are
// padded or cut to the current width (no reflow). Term lock must be held.
func attachViewRow(a *AttachState, y, cols int, cursorX int) string {
	live := func(ly int) func(int) vt10x.Glyph {
		return func(x int) vt10x.Glyph { return a.Term.Cell(x, ly) }
	}
	if a.ScrollOff == 0 {
		return attachGlyphRow(cols, live(y), cursorX)
	}
	n := a.sb.Len()
	idx := n - a.ScrollOff + y
	if idx >= n {
		return attachGlyphRow(cols, live(idx-n), -1)
	}
	line := a.sb.Line(idx)
	return attachGlyphRow(cols, func(x int) vt10x.Glyph {
		if x < len(line) {
			return line[x]
		}
		return blankGlyph
	}, -1)
}

// attachScrollChip is the "SCROLL ↑N/total" title chip.
func attachScrollChip(a *AttachState) string {
	return lipgloss.NewStyle().Bold(true).Foreground(colorNeeds).
		Render(fmt.Sprintf("SCROLL ↑%d/%d", a.ScrollOff, a.sb.Len()))
}

// attachScrollFooter replaces the footer's key hints in scroll mode.
func attachScrollFooter(a *AttachState, width int) string {
	sep := styleMuted.Render("  ·  ")
	left := kbdChip("k/j") + styleDim.Render(" line") + sep +
		kbdChip("^U/^D") + styleDim.Render(" half page") + sep +
		kbdChip("g") + styleDim.Render(" top") + sep +
		kbdChip("G/esc") + styleDim.Render(" live")
	right := attachScrollChip(a)
	if a.scrollNew > 0 {
		right = lipgloss.NewStyle().Foreground(colorNeeds).
			Render(fmt.Sprintf("↓ %d new · G/esc to resume", a.scrollNew))
	}
	pad := width - lipglossWidth(left) - lipglossWidth(right)
	if pad < 1 {
		pad = 1
	}
	return left + strings.Repeat(" ", pad) + right
}

// logAttachScrollbackTotals records capture totals when the popup closes.
func logAttachScrollbackTotals(a *AttachState) {
	if a.sb == nil {
		return
	}
	slog.Info("tui: attach scrollback totals", "session", a.SessionID, "captured", a.sb.captured,
		"evicted", a.sb.evicted, "retained", a.sb.Len(), "bytes", a.sb.bytes, "limit", a.sb.maxLines)
}
