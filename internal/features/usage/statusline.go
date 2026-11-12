package usage

import (
	"encoding/json"
	"math"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// ParseStatusLine extracts the usage fields from one Claude Code status-line
// JSON document (read from stdin by the tap). It keeps only:
//
//	rate_limits.five_hour.{used_percentage,resets_at}
//	rate_limits.seven_day.{used_percentage,resets_at}
//	rate_limits.spend_limit.{used_percentage,resets_at,used_usd,limit_usd,period}
//	cost.total_cost_usd
//	model.id, model.display_name
//	session_id
//
// resets_at is Unix epoch seconds. Every field may be absent, null or of an
// unexpected type; such a field is skipped, never an error, so a Claude
// update that renames fields yields an empty sample instead of a failure.
// ok is false when raw is not a JSON object.
func ParseStatusLine(raw []byte) (s protocol.UsageSample, ok bool) {
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil || doc == nil {
		return s, false
	}
	s.ClaudeSessionID = str(doc, "session_id")
	if rl := obj(doc, "rate_limits"); rl != nil {
		s.FiveHour = window(obj(rl, "five_hour"))
		s.SevenDay = window(obj(rl, "seven_day"))
		if sp := obj(rl, "spend_limit"); sp != nil {
			spend := &protocol.UsageSpend{Period: str(sp, "period"), UsedUSD: num(sp, "used_usd"), LimitUSD: num(sp, "limit_usd")}
			if w := window(sp); w != nil {
				spend.UsageWindow = *w
			}
			if spend.UsedPct != nil || spend.ResetsAt != 0 || spend.UsedUSD != nil || spend.LimitUSD != nil {
				s.Spend = spend
			}
		}
	}
	s.CostUSD = num(obj(doc, "cost"), "total_cost_usd")
	if m := obj(doc, "model"); m != nil {
		s.ModelID = str(m, "id")
		s.ModelName = str(m, "display_name")
	}
	return s, true
}

// window reads used_percentage and resets_at from m, nil when neither is
// usable.
func window(m map[string]any) *protocol.UsageWindow {
	if m == nil {
		return nil
	}
	w := &protocol.UsageWindow{UsedPct: num(m, "used_percentage")}
	if r := num(m, "resets_at"); r != nil && *r > 0 && *r <= 253402300799 {
		w.ResetsAt = int64(*r)
	}
	if w.UsedPct == nil && w.ResetsAt == 0 {
		return nil
	}
	return w
}

func obj(m map[string]any, k string) map[string]any {
	if m == nil {
		return nil
	}
	o, _ := m[k].(map[string]any)
	return o
}

func str(m map[string]any, k string) string {
	if m == nil {
		return ""
	}
	s, _ := m[k].(string)
	return s
}

// num reads a finite, non-negative number; anything else is absent.
func num(m map[string]any, k string) *float64 {
	if m == nil {
		return nil
	}
	f, ok := m[k].(float64)
	if !ok || math.IsNaN(f) || math.IsInf(f, 0) || f < 0 {
		return nil
	}
	return &f
}
