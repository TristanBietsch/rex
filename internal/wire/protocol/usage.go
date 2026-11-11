package protocol

import "time"

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

// EventUsage carries the live usage quotas (see Usage).
const EventUsage = "Usage"

// IntentUsageQuery asks for usage aggregates over a range. The reply is a
// UsageResult event with the intent's ID (UsageReport), not an Ack. An older
// daemon answers bad_intent "intent not implemented"; a daemon with usage
// off answers bad_intent "usage is off".
const IntentUsageQuery = "UsageQuery"

// EventUsageResult is the reply to IntentUsageQuery.
const EventUsageResult = "UsageResult"

// Usage ranges for UsageQuery.Range.
const (
	UsageRangeToday = "today"
	UsageRange7d    = "7d"
	UsageRange30d   = "30d"
)

// ValidUsageRange reports whether r is one of the UsageRange* values.
func ValidUsageRange(r string) bool {
	return r == UsageRangeToday || r == UsageRange7d || r == UsageRange30d
}

// Usage is the live usage state: the latest quota reading per tool and
// window. It is kept in the daemon's memory only (never persisted), sent as
// the Usage event whenever a quota changes and in Snapshot.usage. A Usage
// with Enabled false means the usage setting was switched off: clients drop
// what they held.
type Usage struct {
	Enabled bool         `json:"enabled"`
	Quotas  []UsageQuota `json:"quotas,omitempty"`
}

// UsageQuota is one quota window of one tool.
type UsageQuota struct {
	Tool string `json:"tool"`
	// Model is empty for a plan-wide quota.
	Model string `json:"model,omitempty"`
	// Window is "5h", "week" or a spend period such as "month".
	Window string `json:"window"`
	// UsedPct is 0-100; nil when the tool reports only money.
	UsedPct *float64 `json:"used_pct,omitempty"`
	// ResetsAt is when the window resets; omitted when unknown.
	ResetsAt time.Time `json:"resets_at,omitzero"`
	// UsedUSD and LimitUSD are a spend limit's amounts.
	UsedUSD  *float64 `json:"used_usd,omitempty"`
	LimitUSD *float64 `json:"limit_usd,omitempty"`
	// At is when the daemon last heard this reading. A client treats a
	// reading older than 15 minutes as stale.
	At time.Time `json:"at"`
	// Stale is the daemon's own call of that at the time it sent the value.
	Stale bool `json:"stale,omitempty"`
}

// UsageQuery is the UsageQuery intent.
type UsageQuery struct {
	// Range is a UsageRange* value; empty means "today".
	Range string `json:"range,omitempty"`
	// Tool limits the report to one tool id; empty means all.
	Tool string `json:"tool,omitempty"`
}

// UsageReport is the UsageResult payload: totals over the range per tool,
// per model and overall, from the usage ledger and the live sessions, plus
// the current quotas.
type UsageReport struct {
	Range string `json:"range"`
	// Since is the start of the range.
	Since time.Time `json:"since"`
	Tool  string    `json:"tool,omitempty"`
	Total UsageRow  `json:"total"`
	// Tools is sorted by tokens, most first, then by name.
	Tools  []UsageToolRow `json:"tools"`
	Quotas []UsageQuota   `json:"quotas,omitempty"`
}

// UsageRow is a total.
type UsageRow struct {
	Sessions int   `json:"sessions"`
	Tokens   int64 `json:"tokens"`
	// TokensEstimated is true when any counted tokens are rex's estimate.
	TokensEstimated bool `json:"tokens_estimated,omitempty"`
	// CostUSD is nil when no counted session reported a cost.
	CostUSD *float64 `json:"cost_usd,omitempty"`
}

// UsageToolRow is one tool's total with its models.
type UsageToolRow struct {
	Tool string `json:"tool"`
	UsageRow
	Models []UsageModelRow `json:"models,omitempty"`
}

// UsageModelRow is one model's total. Model is empty when unknown.
type UsageModelRow struct {
	Model string `json:"model"`
	UsageRow
}
