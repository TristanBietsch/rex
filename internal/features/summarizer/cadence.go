package summarizer

import "time"

// Cadence is the schedule of refreshes for one live session, counted from the
// start of its run: EarlyCount refreshes EarlyEvery apart, then one every
// LateEvery. Tick k is 1-based; ticks 1..EarlyCount are the early phase.
type Cadence struct {
	EarlyEvery time.Duration
	EarlyCount int
	LateEvery  time.Duration
}

// DefaultCadence is the shipped schedule (R15): 5, 10, 15, 20 and 25 minutes
// after the run started, then every 30 minutes. It lives here and nowhere
// else; tests pass a scaled-down Config.Cadence.
var DefaultCadence = Cadence{EarlyEvery: 5 * time.Minute, EarlyCount: 5, LateEvery: 30 * time.Minute}

// Due is when tick k (1-based) falls for a run that started at anchor.
func (c Cadence) Due(anchor time.Time, k int) time.Time {
	if k <= c.EarlyCount {
		return anchor.Add(time.Duration(k) * c.EarlyEvery)
	}
	return anchor.Add(time.Duration(c.EarlyCount)*c.EarlyEvery + time.Duration(k-c.EarlyCount)*c.LateEvery)
}

// Late reports whether tick k belongs to the late phase.
func (c Cadence) Late(k int) bool { return k > c.EarlyCount }

// Elapsed is how many ticks are due at or before now.
func (c Cadence) Elapsed(anchor, now time.Time) int {
	d := now.Sub(anchor)
	if d < 0 {
		return 0
	}
	early := time.Duration(c.EarlyCount) * c.EarlyEvery
	if d < early {
		return int(d / c.EarlyEvery)
	}
	return c.EarlyCount + int((d-early)/c.LateEvery)
}

// orDefault maps the zero Cadence to DefaultCadence.
func (c Cadence) orDefault() Cadence {
	if c == (Cadence{}) {
		return DefaultCadence
	}
	return c
}
