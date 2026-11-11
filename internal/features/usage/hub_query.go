package usage

import (
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// Errors a UsageQuery can fail with.
var (
	// ErrOff is the answer while the usage setting is off.
	ErrOff = errors.New("usage is off")
	// ErrBadRange is a Range that is not today, 7d or 30d.
	ErrBadRange = errors.New("range must be today, 7d or 30d")
)

// maxModelRunes caps a model id taken from the tap.
const maxModelRunes = 64

// quotaKey identifies one quota window.
type quotaKey struct{ tool, model, window string }

// Sample takes what the Claude tap sent: it feeds the session's cost and
// model (Report) and keeps the latest rate-limit windows as quotas. A sample
// for a session the hub has not seen, or while it is off, is ignored. When a
// quota changed, OnUsage is called (after the lock is released).
func (h *Hub) Sample(s protocol.UsageSample) {
	if h == nil {
		return
	}
	h.mu.Lock()
	l, ok := h.live[s.SessionID]
	if !h.enabled || !ok {
		h.mu.Unlock()
		return
	}
	// Text from Claude's JSON is untrusted: it ends up in the ledger and in
	// terminals, so it is cut to a short plain label first.
	conv := s.ClaudeSessionID
	if len(conv) > 128 {
		conv = conv[:128]
	}
	h.reportLocked(l, conv, s.CostUSD, protocol.SanitizeDisplayText(s.ModelID, maxModelRunes))
	at := h.cfg.Now().UTC()
	changed := false
	set := func(window string, w *protocol.UsageWindow, used, limit *float64) {
		q := protocol.UsageQuota{Tool: l.sum.ToolID, Window: window, At: at, UsedUSD: used, LimitUSD: limit}
		if w != nil {
			q.UsedPct = w.UsedPct
			if w.ResetsAt > 0 {
				q.ResetsAt = time.Unix(w.ResetsAt, 0).UTC()
			}
		}
		if q.UsedPct == nil && q.UsedUSD == nil && q.LimitUSD == nil && q.ResetsAt.IsZero() {
			return
		}
		k := quotaKey{q.Tool, "", window}
		if old, had := h.quotas[k]; !had || !sameQuota(old, q) {
			changed = true
		}
		if h.quotas == nil {
			h.quotas = map[quotaKey]protocol.UsageQuota{}
		}
		h.quotas[k] = q
	}
	set(Window5h, s.FiveHour, nil, nil)
	set(WindowWeek, s.SevenDay, nil, nil)
	if sp := s.Spend; sp != nil {
		set(spendWindow(sp.Period), &sp.UsageWindow, sp.UsedUSD, sp.LimitUSD)
	}
	var ev protocol.Usage
	if changed {
		ev = h.usageLocked()
	}
	h.mu.Unlock()
	if changed {
		h.publish(ev)
	}
}

// spendWindow names a spend limit's window from its period: a short,
// lowercase word ("month"), "month" when the period is missing or odd. The
// period is untrusted text from Claude's JSON.
func spendWindow(period string) string {
	p := strings.ToLower(strings.TrimSpace(period))
	if p == "" || len(p) > 16 {
		return WindowMonth
	}
	for _, r := range p {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			return WindowMonth
		}
	}
	return p
}

func eqPtr(a, b *float64) bool {
	return (a == nil) == (b == nil) && (a == nil || *a == *b)
}

// sameQuota reports whether b says nothing new over a (only At differs).
func sameQuota(a, b protocol.UsageQuota) bool {
	return eqPtr(a.UsedPct, b.UsedPct) && eqPtr(a.UsedUSD, b.UsedUSD) && eqPtr(a.LimitUSD, b.LimitUSD) && a.ResetsAt.Equal(b.ResetsAt)
}

// publish hands ev to OnUsage.
func (h *Hub) publish(ev protocol.Usage) {
	if h.cfg.OnUsage != nil {
		h.cfg.OnUsage(ev)
	}
}

// usageLocked is the live state as the Usage event.
func (h *Hub) usageLocked() protocol.Usage {
	return protocol.Usage{Enabled: h.enabled, Quotas: h.quotasLocked("")}
}

// quotasLocked lists the current quotas of tool ("" = all), sorted, with
// Stale decided against the clock.
func (h *Hub) quotasLocked(tool string) []protocol.UsageQuota {
	var out []protocol.UsageQuota
	now := h.cfg.Now()
	for _, q := range h.quotas {
		if tool != "" && q.Tool != tool {
			continue
		}
		q.Stale = now.Sub(q.At) > StaleAfter
		out = append(out, q)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Tool != b.Tool {
			return a.Tool < b.Tool
		}
		return windowOrder(a.Window) < windowOrder(b.Window) || (windowOrder(a.Window) == windowOrder(b.Window) && a.Window < b.Window)
	})
	return out
}

func windowOrder(w string) int {
	switch w {
	case Window5h:
		return 0
	case WindowWeek:
		return 1
	}
	return 2
}

// Usage is the current Usage event payload (Enabled false while off).
func (h *Hub) Usage() protocol.Usage {
	if h == nil {
		return protocol.Usage{}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.usageLocked()
}

// RangeStart is the start of range r at now: local midnight for today, the
// last 7 or 30 days for the others. ErrBadRange for any other r; "" is today.
func RangeStart(r string, now time.Time) (time.Time, error) {
	switch r {
	case "", protocol.UsageRangeToday:
		y, m, d := now.Date()
		return time.Date(y, m, d, 0, 0, 0, 0, now.Location()), nil
	case protocol.UsageRange7d:
		return now.AddDate(0, 0, -7), nil
	case protocol.UsageRange30d:
		return now.AddDate(0, 0, -30), nil
	}
	return time.Time{}, ErrBadRange
}

// Query answers a UsageQuery: totals over the range from the ledger (the
// latest line per session) overlaid with the live sessions' current totals,
// so a running session counts now, not as of its last periodic line. A
// session counts in a range when its latest line is inside it.
func (h *Hub) Query(q protocol.UsageQuery) (protocol.UsageReport, error) {
	if h == nil || !h.Enabled() {
		return protocol.UsageReport{}, ErrOff
	}
	rng := q.Range
	if rng == "" {
		rng = protocol.UsageRangeToday
	}
	since, err := RangeStart(rng, h.cfg.Now())
	if err != nil {
		return protocol.UsageReport{}, err
	}
	recs, err := h.cfg.Ledger.Latest()
	if err != nil {
		return protocol.UsageReport{}, err
	}
	h.mu.Lock()
	for id, l := range h.live {
		if l.sum.ToolID == "" {
			continue
		}
		cur := h.recordLocked(l, terminal(l.sum.State))
		if old, ok := recs[id]; !ok || !cur.At.Before(old.At) {
			recs[id] = cur
		}
	}
	quotas := h.quotasLocked(q.Tool)
	h.mu.Unlock()
	return Aggregate(recs, rng, since, q.Tool, quotas), nil
}

// Aggregate totals recs (one latest record per session) over the range that
// starts at since, for tool ("" = all tools).
func Aggregate(recs map[string]Record, rng string, since time.Time, tool string, quotas []protocol.UsageQuota) protocol.UsageReport {
	type acc struct {
		row  protocol.UsageRow
		cost float64
	}
	add := func(a *acc, r Record) {
		a.row.Sessions++
		a.row.Tokens += r.Tokens
		a.row.TokensEstimated = a.row.TokensEstimated || r.Estimated
		if r.CostUSD != nil {
			a.cost += *r.CostUSD
			v := a.cost
			a.row.CostUSD = &v
		}
	}
	var total acc
	tools := map[string]*acc{}
	models := map[string]map[string]*acc{}
	for _, r := range recs {
		if r.At.Before(since) || (tool != "" && r.Tool != tool) {
			continue
		}
		add(&total, r)
		if tools[r.Tool] == nil {
			tools[r.Tool] = &acc{}
			models[r.Tool] = map[string]*acc{}
		}
		add(tools[r.Tool], r)
		if models[r.Tool][r.Model] == nil {
			models[r.Tool][r.Model] = &acc{}
		}
		add(models[r.Tool][r.Model], r)
	}
	rep := protocol.UsageReport{Range: rng, Since: since, Tool: tool, Total: total.row, Tools: []protocol.UsageToolRow{}, Quotas: quotas}
	for name, a := range tools {
		row := protocol.UsageToolRow{Tool: name, UsageRow: a.row}
		for m, ma := range models[name] {
			row.Models = append(row.Models, protocol.UsageModelRow{Model: m, UsageRow: ma.row})
		}
		sort.Slice(row.Models, func(i, j int) bool {
			a, b := row.Models[i], row.Models[j]
			return a.Tokens > b.Tokens || (a.Tokens == b.Tokens && a.Model < b.Model)
		})
		rep.Tools = append(rep.Tools, row)
	}
	sort.Slice(rep.Tools, func(i, j int) bool {
		a, b := rep.Tools[i], rep.Tools[j]
		return a.Tokens > b.Tokens || (a.Tokens == b.Tokens && a.Tool < b.Tool)
	})
	return rep
}
