package protocol

// IntentUsageSample is sent by `rex-daemon statusline-tap` with the usage
// fields of one Claude Code status-line render. Fire and forget: the tap
// sends it with no envelope ID and never waits for a reply.
const IntentUsageSample = "UsageSample"

// UsageSample is the part of Claude Code's status-line JSON rex keeps. Every
// field may be absent: a plan without rate limits has no windows, and an
// older Claude has none of them. Nothing else of the JSON (prompts, paths,
// transcript location) is ever sent to the daemon.
type UsageSample struct {
	// SessionID is the rex session the tap was started for (its --session
	// flag). The daemon ignores a sample for an unknown session.
	SessionID string `json:"session_id"`
	// ClaudeSessionID is Claude's own conversation id (the status line's
	// session_id).
	ClaudeSessionID string `json:"claude_session_id,omitempty"`
	// FiveHour and SevenDay are Claude's rate-limit windows
	// (rate_limits.five_hour and rate_limits.seven_day).
	FiveHour *UsageWindow `json:"five_hour,omitempty"`
	SevenDay *UsageWindow `json:"seven_day,omitempty"`
	// Spend is rate_limits.spend_limit.
	Spend *UsageSpend `json:"spend_limit,omitempty"`
	// CostUSD is cost.total_cost_usd: the conversation's cost so far.
	CostUSD *float64 `json:"cost_usd,omitempty"`
	// ModelID and ModelName are model.id and model.display_name.
	ModelID   string `json:"model_id,omitempty"`
	ModelName string `json:"model_name,omitempty"`
}

// UsageWindow is one rate-limit window.
type UsageWindow struct {
	// UsedPct is used_percentage (0-100); nil when absent or null.
	UsedPct *float64 `json:"used_pct,omitempty"`
	// ResetsAt is resets_at: Unix epoch seconds, 0 when absent or null.
	ResetsAt int64 `json:"resets_at,omitempty"`
}

// UsageSpend is rate_limits.spend_limit: a money budget instead of a
// percentage window.
type UsageSpend struct {
	UsageWindow
	UsedUSD  *float64 `json:"used_usd,omitempty"`
	LimitUSD *float64 `json:"limit_usd,omitempty"`
	// Period names the budget period ("month", ...); free text, sanitized by
	// the daemon.
	Period string `json:"period,omitempty"`
}

// Empty reports whether the sample carries nothing worth sending.
func (s UsageSample) Empty() bool {
	return s.FiveHour == nil && s.SevenDay == nil && s.Spend == nil && s.CostUSD == nil && s.ModelID == "" && s.ModelName == ""
}
