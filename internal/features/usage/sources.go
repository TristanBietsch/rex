package usage

import (
	"context"
	"sort"
	"time"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// SessionLister is how the estimate source sees the board.
type SessionLister func() []protocol.SessionSummary

// EstimateSource turns rex's own per-session token counter into one
// session-scope Reading per session, marked Estimated. It is the fallback
// for every tool that reports nothing itself.
type EstimateSource struct {
	list SessionLister
	now  func() time.Time
}

// NewEstimateSource reads sessions from list.
func NewEstimateSource(list SessionLister) *EstimateSource {
	return &EstimateSource{list: list, now: time.Now}
}

// Name implements Source: the estimate covers every tool.
func (*EstimateSource) Name() string { return "estimate" }

// Poll implements Source.
func (e *EstimateSource) Poll(context.Context) ([]Reading, error) {
	at := e.now()
	var out []Reading
	for _, s := range e.list() {
		if s.Tokens <= 0 {
			continue
		}
		out = append(out, Reading{
			Tool: s.ToolID, Model: s.ModelID, SessionID: s.ID, Window: WindowSession,
			Tokens: s.Tokens, Estimated: true, At: at,
		})
	}
	return out, nil
}

// GrokSource is the registration point for `grok usage`.
//
// TODO(R10-1b): capture `grok usage` (plan and per-session forms) and write
// its parser. The output format has not been captured, so this source parses
// nothing and Poll answers ErrNotImplemented. Do not guess a format.
type GrokSource struct{}

// Name implements Source.
func (GrokSource) Name() string { return "grok" }

// Poll implements Source.
func (GrokSource) Poll(context.Context) ([]Reading, error) { return nil, ErrNotImplemented }

// Sources is the set of registered sources, by Name.
type Sources map[string]Source

// DefaultSources registers every built-in source. The Claude tap is not one
// of them: it is pushed to through the daemon's UsageSample intent (Hub).
func DefaultSources(list SessionLister) Sources {
	s := Sources{}
	for _, src := range []Source{NewEstimateSource(list), GrokSource{}} {
		s[src.Name()] = src
	}
	return s
}

// Names lists the registered source names, sorted.
func (s Sources) Names() []string {
	out := make([]string, 0, len(s))
	for n := range s {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
