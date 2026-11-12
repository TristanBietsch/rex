package tui

import (
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Screensaver.
//
// After screensaver_after_minutes without a key or mouse event, the screen
// gives way to a small centered "∴ rex" with the session counts under it.
// That holds whatever has focus: an overlay, prompt, confirm, the wizard or
// the attach popup left open is still an idle screen. Only the boot splash
// never times out. The focused component keeps its state underneath; the
// next key or mouse event (or a terminal resize) brings it back and does
// nothing else. Daemon events keep the counts live, and an attached
// session's output still reaches its terminal.
//
// One slow timer drives it (idleTickMsg). It fires when the idle deadline
// passes and otherwise at most once a minute, which also picks up a changed
// setting and, while the screensaver shows, moves it a little (drift, off
// with reduce_motion). Nothing ticks faster while the screensaver shows: the
// spinner chain stops (wantsSpinnerTick).

const (
	// idleMaxWait caps one idle timer wait; idleMinWait keeps a deadline
	// that is just about due from re-arming in a tight loop.
	idleMaxWait = time.Minute
	idleMinWait = time.Second

	// saverDriftEvery is how often the screensaver moves; it moves at most
	// saverDriftX columns and saverDriftY rows away from the center.
	saverDriftEvery = time.Minute
	saverDriftX     = 6
	saverDriftY     = 2

	saverLogo = "∴ rex"
)

// idleTickMsg is one tick of the idle timer chain gen.
type idleTickMsg struct{ gen int }

// idleTick waits d, then emits idleTickMsg for chain gen. It returns nothing
// once done closes (the TUI exited), so no timer goroutine outlives the
// program.
func idleTick(gen int, d time.Duration, done <-chan struct{}) tea.Cmd {
	return func() tea.Msg {
		t := time.NewTimer(d)
		defer t.Stop()
		select {
		case <-t.C:
			return idleTickMsg{gen: gen}
		case <-done:
			return nil
		}
	}
}

// armIdle starts the idle timer chain when none is pending. Update calls it
// after every message, like armSpinner, so the chain can't stop for good.
// It needs idleDone and waits out the boot splash.
func armIdle(m Model) (Model, tea.Cmd) {
	if m.idleDone == nil || m.idleArmed || m.Focus == FocusBoot {
		return m, nil
	}
	now := m.now()
	if m.lastInput.IsZero() {
		m.lastInput = now
	}
	m.idleGen++
	m.idleArmed = true
	return m, idleTick(m.idleGen, idleDelay(m, now), m.idleDone)
}

// idleDelay is how long the next idle timer waits: until the screensaver's
// next drift step while it shows, else until the idle deadline, never more
// than idleMaxWait.
func idleDelay(m Model, now time.Time) time.Duration {
	d := idleMaxWait
	switch {
	case m.saverOn:
		if saverMotion(m) {
			d = saverDriftEvery - now.Sub(m.saverSince)%saverDriftEvery
		}
		if saverClockFormat(m) != "" { // the next minute, so the clock is never behind
			d = min(d, now.Truncate(time.Minute).Add(time.Minute).Sub(now))
		}
	case saverEnabled(m):
		if left := m.lastInput.Add(saverAfter(m)).Sub(now); left > 0 {
			d = left
		}
	}
	return min(max(d, idleMinWait), idleMaxWait)
}

// handleIdleTick ends the current chain (Update arms the next one) and
// shows the screensaver if the board has been idle long enough.
func handleIdleTick(m Model, msg idleTickMsg) Model {
	if msg.gen != m.idleGen {
		return m // stale chain
	}
	m.idleArmed = false
	return checkIdle(m, m.now())
}

// checkIdle shows the screensaver when it is on, the boot splash is gone,
// and no key or mouse event came for screensaver_after_minutes. A
// screensaver already showing goes away if the setting was turned off
// meanwhile (a live reload).
func checkIdle(m Model, now time.Time) Model {
	if m.saverOn {
		if !saverEnabled(m) {
			return wakeSaver(m, "disabled")
		}
		return m
	}
	if m.Focus == FocusBoot || !saverEnabled(m) {
		return m
	}
	if now.Sub(m.lastInput) < saverAfter(m) {
		return m
	}
	m.saverOn = true
	m.saverSince = now
	m.PendingChord = "" // a half-typed dd or cc must not finish after waking
	keepAwake := saverKeepAwake(m)
	if keepAwake {
		m.keepAwake.Start()
	}
	slog.Info("tui: screensaver on", "idle", now.Sub(m.lastInput).Round(time.Second), "focus", m.Focus, "keep_awake", keepAwake)
	return m
}

// wakeSaver hides the screensaver and lets the display sleep again. The
// event that woke it is not acted on. Row animations that started under the
// screensaver had no ticks; the board shows their end state instead.
func wakeSaver(m Model, by string) Model {
	m.saverOn = false
	m.keepAwake.Stop()
	m.DescAnim = nil
	m = pruneDoneBlinks(m, time.Now())
	slog.Info("tui: screensaver off", "by", by, "shown", m.now().Sub(m.saverSince).Round(time.Second))
	return m
}

func saverEnabled(m Model) bool {
	if m.Store == nil {
		return false
	}
	b, _ := m.Store.Get("screensaver_enabled").(bool)
	return b
}

func saverAfter(m Model) time.Duration {
	n := 5
	if m.Store != nil {
		if v, ok := m.Store.Get("screensaver_after_minutes").(int); ok && v > 0 {
			n = v
		}
	}
	return time.Duration(n) * time.Minute
}

func saverKeepAwake(m Model) bool {
	if m.Store == nil {
		return false
	}
	b, _ := m.Store.Get("screensaver_keep_awake").(bool)
	return b
}

// saverMotion reports whether the screensaver drifts: screensaver_drift on
// and reduce_motion off.
func saverMotion(m Model) bool {
	if m.Store == nil {
		return true
	}
	rm, _ := m.Store.Get("reduce_motion").(bool)
	drift, ok := m.Store.Get("screensaver_drift").(bool)
	return !rm && (drift || !ok)
}

// saverCountsLayout is screensaver_counts: "horizontal" (default),
// "vertical" or "off".
func saverCountsLayout(m Model) string {
	if m.Store != nil {
		if v, _ := m.Store.Get("screensaver_counts").(string); v != "" {
			return v
		}
	}
	return "horizontal"
}

// saverClockFormat is the time layout for screensaver_clock, "" when off.
func saverClockFormat(m Model) string {
	if m.Store == nil {
		return ""
	}
	switch m.Store.Get("screensaver_clock") {
	case "24h":
		return "15:04"
	case "12h":
		return "3:04 PM"
	}
	return ""
}

// renderScreensaver draws the screensaver: exactly h lines of exactly w
// columns, the block centered (each line on its own) and, with motion,
// drifted a little. Lines wider than w are cut, never wrapped.
func renderScreensaver(m Model, w, h int) string {
	block := saverBlock(m, h)
	widest := 0
	for i, l := range block {
		if ansi.StringWidth(l) > w {
			block[i] = ansi.Truncate(l, w, "…")
		}
		widest = max(widest, ansi.StringWidth(block[i]))
	}
	dx, dy := saverDrift(m, (w-widest)/2, (h-len(block))/2)
	top := clampInt((h-len(block))/2+dy, 0, h-len(block))

	out := make([]string, h)
	blank := strings.Repeat(" ", w)
	for i := range out {
		out[i] = blank
	}
	for i, l := range block {
		lw := ansi.StringWidth(l)
		x := clampInt((w-lw)/2+dx, 0, w-lw)
		out[top+i] = strings.Repeat(" ", x) + l + strings.Repeat(" ", w-x-lw)
	}
	return strings.Join(out, "\n")
}

// saverBlock is the screensaver's lines: the logo, the clock
// (screensaver_clock), a blank, the counts (screensaver_counts) and, while
// the daemon is unreachable, a blank and the reconnect status (the counts
// are stale then). A short terminal drops the blanks first, then the clock;
// vertical counts that still don't fit fall back to one line; then the
// logo goes and the rest is cut.
func saverBlock(m Model, h int) []string {
	logo := styleLogo.Render(saverLogo)
	clock := ""
	if f := saverClockFormat(m); f != "" {
		clock = styleHeaderMeta.Render(m.now().Format(f))
	}
	status := ""
	if m.reconnect != nil {
		status = styleStateFailed.Render(reconnectStatus)
	}
	layouts := []string{saverCountsLayout(m)}
	if layouts[0] == "vertical" {
		layouts = append(layouts, "horizontal")
	}
	var block []string
	for _, layout := range layouts {
		counts := saverCountLines(m, layout)
		for _, try := range []struct{ blanks, clock bool }{{true, true}, {false, true}, {false, false}} {
			block = []string{logo}
			if try.clock && clock != "" {
				block = append(block, clock)
			}
			if len(counts) > 0 {
				if try.blanks {
					block = append(block, "")
				}
				block = append(block, counts...)
			}
			if status != "" {
				if try.blanks {
					block = append(block, "")
				}
				block = append(block, status)
			}
			if len(block) <= h {
				return block
			}
		}
	}
	block = block[1:]
	return block[:min(len(block), max(h, 0))]
}

// saverCountLines is the counts in the given screensaver_counts layout:
// one line (saverCounts) for "horizontal", one line per count for
// "vertical" (numbers right-aligned in one column, every line padded to the
// widest so the block centers as a unit), none for "off".
func saverCountLines(m Model, layout string) []string {
	switch layout {
	case "off":
		return nil
	case "vertical":
	default:
		return []string{saverCounts(m)}
	}
	type part struct {
		n     int
		label string
		style lipgloss.Style
	}
	cols, failed := columnCounts(scopedSessions(m), rowFilterOf(m).ctx)
	parts := make([]part, 0, len(cols)+1)
	for gi, n := range cols {
		g := boardGroups[gi]
		if g.HideZero && n == 0 {
			continue
		}
		style := styleHeaderMeta
		if g.Alert && n > 0 {
			style = styleAlert
		}
		parts = append(parts, part{n, g.Count, style})
	}
	if failed > 0 {
		parts = append(parts, part{failed, "failed", styleStateFailed})
	}
	numW, lineW := 0, 0
	for _, p := range parts {
		numW = max(numW, len(strconv.Itoa(p.n)))
	}
	for _, p := range parts {
		lineW = max(lineW, numW+1+ansi.StringWidth(p.label))
	}
	out := make([]string, len(parts))
	for i, p := range parts {
		num := strconv.Itoa(p.n)
		text := p.style.Render(num + " " + p.label)
		lead := strings.Repeat(" ", numW-len(num))
		out[i] = lead + text + strings.Repeat(" ", lineW-len(lead)-len(num)-1-ansi.StringWidth(p.label))
	}
	return out
}

// saverCounts is "1 ready · 3 working · 7 done": one count per board
// column in board order (boardGroups), an Alert column highlighted while it
// holds any session, and "· N failed" when something failed. The numbers
// match the header's.
func saverCounts(m Model) string {
	cols, failed := columnCounts(scopedSessions(m), rowFilterOf(m).ctx)
	parts := make([]string, 0, len(cols)+1)
	for gi, n := range cols {
		g := boardGroups[gi]
		if g.HideZero && n == 0 {
			continue
		}
		style := styleHeaderMeta
		if g.Alert && n > 0 {
			style = styleAlert
		}
		parts = append(parts, style.Render(fmt.Sprintf("%d %s", n, g.Count)))
	}
	if failed > 0 {
		parts = append(parts, styleStateFailed.Render(fmt.Sprintf("%d failed", failed)))
	}
	return strings.Join(parts, styleHeaderMeta.Render(" · "))
}

// saverDrift is the screensaver's offset from the center: (0, 0) when it
// appears and with reduce_motion, then a slow wander that moves once per
// saverDriftEvery and stays within the slack the terminal leaves around
// the block.
func saverDrift(m Model, slackX, slackY int) (dx, dy int) {
	if !saverMotion(m) {
		return 0, 0
	}
	step := float64(m.now().Sub(m.saverSince) / saverDriftEvery)
	ax := float64(clampInt(slackX, 0, saverDriftX))
	ay := float64(clampInt(slackY, 0, saverDriftY))
	return int(math.Round(ax * math.Sin(step*0.5))), int(math.Round(ay * math.Sin(step*0.3)))
}

// clampInt bounds v to [lo, hi]; hi below lo yields lo.
func clampInt(v, lo, hi int) int {
	return max(lo, min(v, hi))
}
