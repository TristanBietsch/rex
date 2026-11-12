// Package tui is the Rex Bubble Tea interface.
package tui

import (
	"log/slog"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/features/audio"
)

// boardPad is the side margin we leave so content isn't flush against the terminal edges.
const boardPad = 0

// Run opens the board. quietBoot silences the boot splash (see
// Model.QuietBoot).
func Run(socket string, quietBoot bool) error { return RunScoped(socket, quietBoot, nil) }

// RunScoped is Run for a board fixed to one workspace (`rex here`); a nil
// scope is the ordinary board.
func RunScoped(socket string, quietBoot bool, scope *WorkspaceScope) error {
	return RunWith(socket, RunOptions{QuietBoot: quietBoot, Scope: scope})
}

// RunOptions are RunWith's options.
type RunOptions struct {
	QuietBoot bool
	Scope     *WorkspaceScope
	// SwapState is the swap state file a hot swap left (SwapEnv); the board
	// restores that view. Empty for a normal start.
	SwapState string
	// Notice is shown on the status line once the board is up.
	Notice string
}

// RunWith opens the board. It returns a *SwapRequest when the board quit to
// swap into a new binary.
func RunWith(socket string, opts RunOptions) error {
	quietBoot, scope := opts.QuietBoot, opts.Scope
	// Synchronous pre-bootstrap: read audio prefs so the Player exists by the
	// time the first step msg arrives. Best-effort; defaults on error.
	store, _, _ := settings.LoadDefault()
	soundEnabled, _ := store.Get("sound_enabled").(bool)
	soundset, _ := store.Get("soundset").(string)
	volume := settings.VolumeGain(store.Get("master_volume"))
	bootVoice, _ := store.Get("boot_voice").(bool)
	if soundset == "off" {
		soundEnabled = false
	}
	player := audio.New(audio.Config{Enabled: soundEnabled, Volume: volume, Soundset: soundset, BootVoice: bootVoice})
	// The auto theme asks the terminal for its background here, before Bubble
	// Tea reads stdin (a reply arriving later would be eaten as keys), and
	// only when theme is auto.
	termLight := bootTermLight(store)

	awake := newDisplayAwake()
	idleDone := make(chan struct{})
	m := Model{
		Socket:    socket,
		Focus:     FocusBoot,
		Filter:    "all",
		Audio:     player,
		BootStart: time.Now(),
		idleDone:  idleDone,
		keepAwake: awake,
		QuietBoot: quietBoot,

		BorderSeed: launchFolder(),
		termLight:  termLight,
		WS:         scope,
	}
	if scope != nil && scope.Root != "" {
		m.BorderSeed = scope.Root // a workspace keeps its color even without one set
	}
	if b, ok := ownBinary(); ok {
		m.bin = b
	}
	if opts.SwapState != "" {
		if st, err := readSwapState(opts.SwapState); err != nil {
			slog.Warn("tui: swap state not restored", "err", err)
		} else {
			m.swapIn = &st
		}
	}
	m.Notice = opts.Notice

	p := tea.NewProgram(m, tea.WithAltScreen())
	return runProgram(p.Run, awake, idleDone)
}

// runProgram runs the TUI and, however it ends (quit, error, a signal, a
// panic Bubble Tea recovered), lets the display sleep again and ends the
// screensaver's pending idle timer.
func runProgram(run func() (tea.Model, error), awake *displayAwake, idleDone chan struct{}) error {
	defer close(idleDone)
	defer awake.Stop()
	final, err := run()
	if err != nil {
		return err
	}
	if fm, ok := final.(Model); ok && fm.BootFailed {
		return fm.BootError
	}
	if fm, ok := final.(Model); ok && fm.swapPath != "" {
		return &SwapRequest{Path: fm.swapPath}
	}
	return nil
}

func (m Model) Init() tea.Cmd {
	if m.Focus == FocusBoot {
		first := nextStep(m)
		return tea.Batch(tea.HideCursor, bootMinTick(), delayThen(first), tickSpinner(m.spinnerGen))
	}
	return tea.Batch(tea.HideCursor, listenDaemon(m.Client), tickSpinner(m.spinnerGen))
}

// View draws the layout area and, when shown, the window frame around it.
// The screensaver is full-bleed: it fills the whole terminal and no frame.
func (m Model) View() string {
	if m.Quitting {
		return ""
	}
	if m.saverOn && m.TermW > 0 {
		m.Width, m.Height = m.TermW, m.TermH
	}
	body, w := m.viewRaw()
	if !frameShown(m) {
		return clipWidth(body, w)
	}
	// wrapFrame cuts every line to the layout width itself, so a framed view
	// measures each line once.
	return wrapFrame(m, body)
}

// viewRaw renders the layout area and returns it with its width. Lines can
// be wider than the width on a tiny screen (an overlay's minimum size):
// clipWidth or wrapFrame cut them.
func (m Model) viewRaw() (string, int) {
	w, h := m.Width, m.Height
	if w <= 0 {
		w = 100
	}
	if h <= 0 {
		h = 32
	}
	return m.view(w, h), w
}

// clipWidth is the last guard: no line leaves View wider than the terminal,
// whatever an overlay's minimum size is on a tiny screen.
func clipWidth(s string, w int) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if !fitsWidth(l, w) {
			lines[i] = ansi.Truncate(l, w, "")
		}
	}
	return strings.Join(lines, "\n")
}

// fitsWidth reports whether s is at most w columns wide. No grapheme is
// wider than its UTF-8 bytes (ASCII is 1 byte, 1 column; wide runes are 3+
// bytes, 2 columns; escapes are 0 columns), so a string of at most w bytes
// fits without measuring it.
func fitsWidth(s string, w int) bool {
	return len(s) <= w || ansi.StringWidth(s) <= w
}

func (m Model) view(w, h int) string {
	if m.saverOn {
		return renderScreensaver(m, w, h)
	}

	switch m.Focus {
	case FocusBoot:
		return renderSplash(m, w, h)
	case FocusWizard:
		return centerPanel(w, h, fitOverlay(renderWizard(m), w), dimBoard(renderFullScreen(m, w, h)))
	case FocusHelp:
		if pagesOn(m) {
			return renderHelpPage(m, w, h)
		}
		return centerOverlayBox(w, h, renderHelp(m, w, h), dimBoard(renderFullScreen(m, w, h)))
	case FocusSettings:
		if pagesOn(m) {
			return renderSettingsPage(m, w, h)
		}
		return centerOverlayBox(w, h, renderSettings(m, w), dimBoard(renderFullScreen(m, w, h)))
	case FocusAttach:
		return centerAttach(m, w, h, attachBackground(m, w, h))
	case FocusFail:
		return centerPanel(w, h, fitOverlay(renderFail(m), w), dimBoard(renderFullScreen(m, w, h)))
	case FocusStats:
		return centerPanel(w, h, fitOverlay(renderStatsOverlay(m), w), dimBoard(renderFullScreen(m, w, h)))
	case FocusOverview:
		return centerPanel(w, h, fitOverlay(renderOverviewOverlay(m), w), dimBoard(renderFullScreen(m, w, h)))
	case FocusLoops:
		return centerPanel(w, h, fitOverlay(renderLoopsOverlay(m), w), dimBoard(renderFullScreen(m, w, h)))
	case FocusUndo:
		return centerPanel(w, h, fitOverlay(renderUndo(m), w), dimBoard(renderFullScreen(m, w, h)))
	case FocusUsage:
		return centerOverlayBox(w, h, renderUsage(m, w, h), dimBoard(renderFullScreen(m, w, h)))
	case FocusCommand:
		// A command bar opened from inside the attach popup (::) keeps the
		// popup on screen; the bar renders in its footer.
		if m.Attach != nil {
			return centerAttach(m, w, h, attachBackground(m, w, h))
		}
	default:
		// The board and its bottom-bar prompts render on the full screen.
	}

	return renderFullScreen(m, w, h)
}

// contentWidth returns the inner board content width (full terminal width).
func contentWidth(termWidth int) int {
	w := termWidth - 2*boardPad
	if w < 40 {
		return termWidth
	}
	return w
}

func renderFullScreen(m Model, w, h int) string {
	cw := contentWidth(w)

	header := renderHeader(m, cw)
	hr := renderHR(cw)
	helpline := renderHelpLine(m, cw)

	var bottom string
	switch m.Focus {
	case FocusConfirmQuit:
		bottom = hr + "\n" + renderQuitConfirm(cw) + "\n" + helpline
	case FocusConfirmDelete:
		bottom = hr + "\n" + renderDeleteConfirm(m, cw) + "\n" + helpline
	case FocusConfirmLoopStop:
		bottom = hr + "\n" + renderLoopStopConfirm(m, cw) + "\n" + helpline
	case FocusRename:
		bottom = hr + "\n" + renderRenamePrompt(m, cw) + "\n" + helpline
	default:
		bottom = hr + "\n" + renderPrompt(m, cw) + "\n" + helpline
	}

	// 1 blank + header + blank + hr + blank + board + preview + 3 bottom = h
	// (boardTop/boardHeightFor/boardBottomRows).
	boardH := boardHeightFor(m, h)
	m.Height = h

	board := renderBoard(m, cw, boardH)

	lines := []string{""}
	lines = append(lines, strings.Split(header, "\n")...)
	lines = append(lines, "", hr, "")
	lines = append(lines, strings.Split(board, "\n")...)
	lines = append(lines, renderDescPreview(m, cw)...)
	lines = append(lines, strings.Split(bottom, "\n")...)

	// Hard cap: never produce more than h lines (the alt-screen window).
	if len(lines) > h {
		lines = lines[:h]
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

// centerPanel composites an overlay's content as a titled panel centered
// over bg: the content's first line becomes the title in the top border.
// The board behind it stays visible around the popup so it reads as an
// overlay, not a separate screen.
func centerPanel(w, h int, content, bg string) string {
	title, body := splitTitle(content)
	popupLines, popupW := panelLines(title, "", body, 2, 0, 0)
	return compositeOverlay(w, h, popupLines, popupW, bg)
}

// centerAttach composites the attach popup (a padded box, or the mobile
// sheet) over bg with the geometry attachChrome gives the mouse.
func centerAttach(m Model, w, h int, bg string) string {
	c := attachChromeOf(m).withBanner(m.Attach.BannerRows)
	popupLines, popupW := overlayBoxPad(renderAttach(m), c.padX(), c.padY())
	return compositeOverlay(w, h, popupLines, popupW, bg)
}

// attachBackground is the board behind the attach popup. A busy agent
// redraws the popup up to ~30 times a second, and nothing on the board
// changes with its output, so the board is rendered once and reused until
// Update sees any other message (keys, daemon events, spinner and timer
// ticks, resizes), which drops it (dropAttachBackground).
func attachBackground(m Model, w, h int) string {
	a := m.Attach
	if a.bg.ok && a.bg.w == w && a.bg.h == h {
		return a.bg.s
	}
	s := renderFullScreen(m, w, h)
	a.bg = attachBgCache{w: w, h: h, s: s, ok: true}
	return s
}

// dropAttachBackground forgets the cached board behind the attach popup
// unless msg is agent output, the one message that never changes the board.
func dropAttachBackground(m Model, msg tea.Msg) {
	if m.Attach == nil {
		return
	}
	if _, ok := msg.(AttachOutputMsg); ok {
		return
	}
	m.Attach.bg = attachBgCache{}
}

// overlayChrome is the columns overlayBox adds around its content: two for the
// border and four for the padding.
const overlayChrome = 6

// fitOverlay cuts every line of overlay content to what overlayBox can show
// on a w-column screen, so the popup is never wider than the terminal. Lines
// that already fit are untouched.
func fitOverlay(content string, w int) string {
	limit := max(w-overlayChrome, 1)
	lines := strings.Split(content, "\n")
	for i, l := range lines {
		if !fitsWidth(l, limit) {
			lines[i] = ansi.Truncate(l, limit, "…")
		}
	}
	return strings.Join(lines, "\n")
}

// centerOverlayBox composites an already-rendered box centered over bg.
func centerOverlayBox(w, h int, box, bg string) string {
	popupLines := strings.Split(box, "\n")
	popupW := 0
	for _, l := range popupLines {
		popupW = max(popupW, ansi.StringWidth(l))
	}
	return compositeOverlay(w, h, popupLines, popupW, bg)
}

// compositeOverlay draws popupLines (each popupW wide) centered over bg.
func compositeOverlay(w, h int, popupLines []string, popupW int, bg string) string {
	bgLines := strings.Split(bg, "\n")
	for len(bgLines) < h {
		bgLines = append(bgLines, "")
	}
	// A popup wider than the screen is clipped: a line past the right edge
	// wraps and shifts every row below it.
	popupW = min(popupW, max(w, 0))
	popupH := len(popupLines)

	x := (w - popupW) / 2
	if x < 0 {
		x = 0
	}
	y := (h - popupH) / 2
	if y < 0 {
		y = 0
	}

	for i, pl := range popupLines {
		row := y + i
		if row >= len(bgLines) {
			break
		}
		bgLine := bgLines[row]
		bgW := ansi.StringWidth(bgLine)
		if bgW < x+popupW {
			bgLine += strings.Repeat(" ", x+popupW-bgW)
		}
		// Exact columns on both sides: a wide board rune straddling an edge
		// used to shift the popup row (left cut) or widen the line (right
		// cut) by one column.
		bgLines[row] = fitColumns(bgLine, x) + fitColumns(pl, popupW) + cutLeft(bgLine, x+popupW)
	}

	return strings.Join(bgLines, "\n")
}

// fitColumns cuts s to at most n columns and pads it to exactly n; a wide
// rune straddling column n is dropped and its column padded.
func fitColumns(s string, n int) string {
	if n <= 0 {
		return ""
	}
	s = ansi.Truncate(s, n, "")
	if w := ansi.StringWidth(s); w < n {
		s += strings.Repeat(" ", n-w)
	}
	return s
}

// cutLeft drops the first n columns of s and keeps exactly the width that
// remains; a wide rune straddling column n becomes a space (ansi.TruncateLeft
// keeps it, one column too many).
func cutLeft(s string, n int) string {
	want := max(ansi.StringWidth(s)-n, 0)
	r := ansi.TruncateLeft(s, n, "")
	if ansi.StringWidth(r) > want {
		r = " " + ansi.TruncateLeft(s, n+1, "")
	}
	return r
}

// overlayBox draws content in a square border with one blank line above and
// below and two columns each side, every line padded to the same width. It
// matches lipgloss Border(NormalBorder()).Padding(1, 2) but measures each
// line once; lipgloss measured them several times, which was most of the
// attach popup's frame time. It returns the box lines and their width.
func overlayBox(content string) ([]string, int) {
	return overlayBoxPad(content, 2, 1)
}

// overlayBoxPad is overlayBox with padX columns and padY rows of padding
// (the attach sheet uses none).
func overlayBoxPad(content string, padX, padY int) ([]string, int) {
	content = strings.ReplaceAll(content, "\r\n", "\n") // as lipgloss does
	content = strings.ReplaceAll(content, "\t", "    ") // lipgloss's tab width
	lines := strings.Split(content, "\n")
	widths := make([]int, len(lines))
	inner := 0
	for i, l := range lines {
		widths[i] = ansi.StringWidth(l)
		inner = max(inner, widths[i])
	}
	padded := inner + 2*padX
	border := styleBorderFg
	side := border.Render("│")
	blank := side + strings.Repeat(" ", padded) + side
	padding := strings.Repeat(" ", padX)
	rule := strings.Repeat("─", padded)

	out := make([]string, 0, len(lines)+4)
	out = append(out, border.Render("┌"+rule+"┐"))
	for range padY {
		out = append(out, blank)
	}
	for i, l := range lines {
		out = append(out, side+padding+l+strings.Repeat(" ", inner-widths[i])+padding+side)
	}
	for range padY {
		out = append(out, blank)
	}
	out = append(out, border.Render("└"+rule+"┘"))
	return out, padded + 2
}
