package tui

import (
	"strings"
	"time"
)

// DescAnim is the active animation for a single session's description cell.
type DescAnim struct {
	From      string
	To        string
	Effect    string // "typewriter" | "decode" | "wipe"
	StartedAt time.Time
	Duration  time.Duration
}

const noiseAlphabet = "!@#$%&*+=?<>~/\\"

// Active reports whether the animation should still render at `now`.
func (a DescAnim) Active(now time.Time) bool {
	return now.Before(a.StartedAt.Add(a.Duration))
}

// renderAnimFrame returns a width-padded rendering of the animation at `now`.
// Output is always exactly `width` runes wide.
func renderAnimFrame(a DescAnim, width int, now time.Time) string {
	if width <= 0 {
		return ""
	}
	p := float64(now.Sub(a.StartedAt)) / float64(a.Duration)
	if p < 0 {
		p = 0
	}
	if p > 1 {
		p = 1
	}
	to := padOrTruncateRunes(a.To, width)
	from := padOrTruncateRunes(a.From, width)
	switch a.Effect {
	case "decode":
		return decodeFrame(to, p, now)
	case "wipe":
		return wipeFrame(to, from, p, width)
	case "typewriter":
		fallthrough
	default:
		return typewriterFrame(to, p, width)
	}
}

func typewriterFrame(to []rune, p float64, width int) string {
	n := int(float64(len(to)) * p)
	if n > len(to) {
		n = len(to)
	}
	var b strings.Builder
	b.Grow(width)
	for i := 0; i < width; i++ {
		if i < n && i < len(to) {
			b.WriteRune(to[i])
		} else {
			b.WriteByte(' ')
		}
	}
	return b.String()
}

func decodeFrame(to []rune, p float64, now time.Time) string {
	noise := []rune(noiseAlphabet)
	var b strings.Builder
	for i, target := range to {
		settle := float64(i+1) / float64(len(to))
		if target == ' ' || p >= settle {
			b.WriteRune(target)
			continue
		}
		idx := int(now.UnixMilli()/40+int64(i)) % len(noise)
		if idx < 0 {
			idx += len(noise)
		}
		b.WriteRune(noise[idx])
	}
	return b.String()
}

func wipeFrame(to, from []rune, p float64, width int) string {
	cursor := int(float64(width) * p)
	if cursor > width {
		cursor = width
	}
	var b strings.Builder
	b.Grow(width)
	for i := 0; i < width; i++ {
		switch {
		case i < cursor:
			b.WriteRune(safeAt(to, i))
		case i == cursor && cursor < width:
			b.WriteRune('█')
		default:
			b.WriteRune(safeAt(from, i))
		}
	}
	return b.String()
}

func padOrTruncateRunes(s string, width int) []rune {
	rs := []rune(s)
	if len(rs) >= width {
		return rs[:width]
	}
	out := make([]rune, width)
	copy(out, rs)
	for i := len(rs); i < width; i++ {
		out[i] = ' '
	}
	return out
}

func safeAt(r []rune, i int) rune {
	if i < 0 || i >= len(r) {
		return ' '
	}
	return r[i]
}
