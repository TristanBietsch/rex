package tui

import (
	"strings"
	"time"
)

// selectorIdle is how long after the last key or mouse event the board
// selector keeps blinking; then it holds solid so the tick chain can rest.
const selectorIdle = time.Minute

// selectorBlinking reports whether the board selector animates now: the
// selector_blink setting is on, reduce_motion is off, the board has focus,
// the screensaver is off and there was input in the last selectorIdle.
func (m Model) selectorBlinking(now time.Time) bool {
	if m.Store == nil || m.saverOn || m.Focus != FocusBoard || m.lastInput.IsZero() {
		return false
	}
	if mode := m.Store.String("selector_blink"); mode != "blink" && mode != "pulse" {
		return false
	}
	if rm, _ := m.Store.Get("reduce_motion").(bool); rm {
		return false
	}
	return now.Sub(m.lastInput) < selectorIdle
}

// selectorHalf is half a selector_blink_ms period, rounded to the 100 ms
// spinner tick that redraws it.
func selectorHalf(m Model) time.Duration {
	ms, _ := m.Store.Get("selector_blink_ms").(int)
	half := time.Duration(ms/2) * time.Millisecond
	return max(half.Round(100*time.Millisecond), 100*time.Millisecond)
}

// selectorMark is the board selector cell, slot columns wide. The blink
// phase counts from the last input, so the selector is solid right after
// you move it, like a text cursor; its off phase keeps the cell's width.
func selectorMark(m Model, slot int) string {
	pad := strings.Repeat(" ", max(0, slot-selectSlotW()))
	now := m.now()
	if m.selectorBlinking(now) && (now.Sub(m.lastInput)/selectorHalf(m))%2 == 1 {
		if m.Store.String("selector_blink") == "blink" {
			return strings.Repeat(" ", slot)
		}
		return styleDim.Bold(curSelector.bold).Render(selectGlyph()) + pad
	}
	return selectArrow() + pad
}
