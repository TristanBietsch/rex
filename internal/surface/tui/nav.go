package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// The full-screen pages (settings, help, new agent) share one vim-style
// list navigator so their lists move the same way:
//
//   - j/k and ↓/↑ move one row and clamp at the ends (no wrap);
//   - gg / home and G / end go to the first and last row;
//   - ctrl+d / ctrl+u move half a window, ctrl+f / ctrl+b (pgdown / pgup)
//     a whole one;
//   - the window scrolls one row at a time and keeps navScrollOff rows of
//     context above and below the cursor (fewer at the list's ends); the
//     "↑ more" / "↓ more" markers take rows of their own, so a marker
//     never hides a row.
//
// Counts (5j) are left out: the digits 1–9 jump to a section on every page.

// navScrollOff is the rows of context kept above and below a list cursor.
const navScrollOff = 2

// navDefaultH is the window height assumed before a list was first drawn.
const navDefaultH = 10

// navMove is a motion decoded from a key by vimMotion.
type navMove int

const (
	navNone     navMove = iota
	navDown             // j, ↓
	navUp               // k, ↑
	navFirst            // gg, home
	navLast             // G, end
	navHalfDown         // ctrl+d
	navHalfUp           // ctrl+u
	navPageDown         // ctrl+f, pgdown
	navPageUp           // ctrl+b, pgup
	navPending          // the first g of gg: the key is used, nothing moves yet
)

// vimMotion decodes key k (tea.KeyMsg.String()) as a list motion. pending
// carries the first g of gg from one key to the next: any other key drops
// it and is decoded on its own.
func vimMotion(k string, pending *bool) navMove {
	if *pending {
		*pending = false
		if k == "g" {
			return navFirst
		}
	}
	switch k {
	case "j", "down":
		return navDown
	case "k", "up":
		return navUp
	case "g":
		*pending = true
		return navPending
	case "home":
		return navFirst
	case "G", "end":
		return navLast
	case "ctrl+d":
		return navHalfDown
	case "ctrl+u":
		return navHalfUp
	case "ctrl+f", "pgdown":
		return navPageDown
	case "ctrl+b", "pgup":
		return navPageUp
	}
	return navNone
}

// navStep is the signed distance mv moves in a window h rows tall (0 for
// navFirst, navLast and navNone).
func navStep(mv navMove, h int) int {
	if h <= 0 {
		h = navDefaultH
	}
	switch mv {
	case navDown:
		return 1
	case navUp:
		return -1
	case navHalfDown:
		return max(h/2, 1)
	case navHalfUp:
		return -max(h/2, 1)
	case navPageDown:
		return max(h-1, 1)
	case navPageUp:
		return -max(h-1, 1)
	case navNone, navFirst, navLast, navPending:
	}
	return 0
}

// navTarget is the row mv takes cursor cur to in a list of n rows drawn h
// rows tall: clamped to the list, never wrapped.
func navTarget(mv navMove, cur, n, h int) int {
	if n <= 0 {
		return 0
	}
	switch mv {
	case navFirst:
		return 0
	case navLast:
		return n - 1
	case navNone, navDown, navUp, navHalfDown, navHalfUp, navPageDown, navPageUp, navPending:
	}
	return min(max(cur+navStep(mv, h), 0), n-1)
}

// listView is a list window kept between draws: Top is the first row shown
// and H the height it was last drawn at (ctrl+d's half page). Key is what
// the list showed: a draw of another list (another section, another tool)
// starts from the top.
type listView struct {
	Top, H int
	Key    string
}

// navSpan is the rows shown in a window h tall that starts at top over n
// rows: [start, end), and whether the ↑ / ↓ markers take a row.
func navSpan(top, n, h int) (start, end int, up, down bool) {
	if n <= h {
		return 0, n, false, false
	}
	up = top > 0
	avail := h
	if up {
		avail--
	}
	if top+avail < n {
		down = true
		avail--
	}
	return top, top + max(avail, 1), up, down
}

// navScroll is the first row to show for cursor cur over n rows in a window
// h tall, starting from the last top: the least scroll that keeps the cursor
// navScrollOff rows away from a marker or the window's edge (fewer at the
// list's ends, and on a window too short for it), and leaves no blank rows
// under the last one.
func navScroll(top, cur, n, h int) int {
	if n <= h || h <= 0 {
		return 0
	}
	cur = min(max(cur, 0), n-1)
	off := min(navScrollOff, max((h-3)/2, 0))
	top = min(max(top, 0), n-1)
	// Jump near a far cursor first; the loop below then fine-tunes.
	if cur < top {
		top = max(cur-off-1, 0)
	} else if cur >= top+h {
		top = max(cur-h+off+2, 0)
	}
	for range 2*h + 4 {
		start, end, _, _ := navSpan(top, n, h)
		switch {
		case top > 0 && cur < start+off:
			top--
		case end < n && cur > end-1-off:
			top++
		case top > 0 && n-top < h-1:
			top-- // the list got shorter (or the window taller): fill the window
		default:
			return top
		}
	}
	return top
}

// window draws lines in at most h rows around cursor cur, scrolling from the
// last draw (Top) as navScroll does and remembering it; the markers are
// indented by ind. A different key starts from the top.
func (v *listView) window(key string, lines []string, cur, h int, ind string) []string {
	if v.Key != key {
		v.Key, v.Top = key, 0
	}
	h = max(h, 1)
	v.H = h
	if len(lines) <= h {
		v.Top = 0
		return lines
	}
	v.Top = navScroll(v.Top, cur, len(lines), h)
	start, end, up, down := navSpan(v.Top, len(lines), h)
	out := make([]string, 0, h)
	if up {
		out = append(out, styleDim.Render(ind+"↑ more"))
	}
	out = append(out, lines[start:end]...)
	if down {
		out = append(out, styleDim.Render(ind+"↓ more"))
	}
	return out
}

// wheelDelta is a wheel event's step: the same one row as j/k.
func wheelDelta(msg tea.MouseMsg) int {
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		return -1
	case tea.MouseButtonWheelDown:
		return 1
	default:
		return 0
	}
}

// wheelMotion is a wheel event as a list motion: navUp or navDown.
func wheelMotion(msg tea.MouseMsg) navMove {
	switch wheelDelta(msg) {
	case -1:
		return navUp
	case 1:
		return navDown
	}
	return navNone
}
