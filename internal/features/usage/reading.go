// Package usage tracks what the agents have used: quota windows the tools
// report, a per-session token and cost ledger, and the aggregates behind
// `rex usage`.
//
// Sources produce Readings. The Claude status-line tap (tap.go) feeds the
// daemon live; rex's own token counter is the estimate for every other tool.
// The ledger (ledger.go) is what survives a pruned session, so a 7 or 30 day
// view does not depend on live state.
package usage

import (
	"context"
	"errors"
	"time"
)

// Window names a quota window or a reading's scope.
const (
	Window5h      = "5h"
	WindowWeek    = "week"
	WindowMonth   = "month"
	WindowSession = "session"
)

// Reading is one usage fact from one source.
type Reading struct {
	Tool string
	// Model is empty for a plan-wide quota and set for a per-model quota or
	// a per-session cost.
	Model     string
	SessionID string
	// Window is Window5h, WindowWeek, WindowMonth or WindowSession.
	Window string
	// UsedPct is 0-100; meaningful only for a quota window (HasPct).
	UsedPct float64
	HasPct  bool
	// ResetsAt is zero when the tool reports no reset time.
	ResetsAt time.Time
	// CostUSD is the cost so far (a session's, or a spend limit's used
	// amount). LimitUSD is a spend limit's cap, 0 when none.
	CostUSD  float64
	LimitUSD float64
	Tokens   int64
	// Estimated marks a value that is rex's own guess, shown with "~".
	Estimated bool
	// Stale marks a reading older than StaleAfter; shown dimmed.
	Stale bool
	At    time.Time
}

// StaleAfter is how old a live reading may get before it is Stale.
const StaleAfter = 15 * time.Minute

// Source is one place usage comes from.
type Source interface {
	// Name is the tool id the source reports for ("claude", "grok", ...).
	Name() string
	// Poll returns the source's current readings. A source that is pushed
	// to (the Claude tap) returns what it last received.
	Poll(ctx context.Context) ([]Reading, error)
}

// ErrNotImplemented is what a registered source answers while its output
// format has not been captured yet.
var ErrNotImplemented = errors.New("usage source not implemented")
