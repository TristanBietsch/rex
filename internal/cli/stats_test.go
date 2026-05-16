package cli

import (
	"testing"
	"time"

	"github.com/tristanbietsch/rex/internal/protocol"
)

func TestAggregateSessionsEmpty(t *testing.T) {
	agg := aggregateSessions(nil)
	if agg.totals.Sessions != 0 {
		t.Errorf("expected 0 sessions, got %d", agg.totals.Sessions)
	}
	if len(agg.byModel) != 0 {
		t.Errorf("expected 0 model rows, got %d", len(agg.byModel))
	}
	if len(agg.byTool) != 0 {
		t.Errorf("expected 0 tool rows, got %d", len(agg.byTool))
	}
}

func TestAggregateSessionsCounts(t *testing.T) {
	now := time.Now()
	sessions := []protocol.SessionSummary{
		{ID: "1", ToolID: "claude", ModelID: "sonnet", State: protocol.StateDone, StartedAt: now.Add(-2 * time.Hour), LastEventAt: now.Add(-1 * time.Hour)},
		{ID: "2", ToolID: "claude", ModelID: "sonnet", State: protocol.StateFailed, StartedAt: now.Add(-30 * time.Minute), LastEventAt: now.Add(-20 * time.Minute)},
		{ID: "3", ToolID: "codex", ModelID: "gpt-5.2", State: protocol.StateDone, StartedAt: now.Add(-3 * time.Hour), LastEventAt: now.Add(-2 * time.Hour)},
		{ID: "4", ToolID: "ollama", ModelID: "llama3", State: protocol.StateWorking, StartedAt: now.Add(-10 * time.Minute), LastEventAt: now.Add(-1 * time.Minute)},
	}

	agg := aggregateSessions(sessions)

	if agg.totals.Sessions != 4 {
		t.Errorf("expected 4 total sessions, got %d", agg.totals.Sessions)
	}
	if agg.totals.Done != 2 {
		t.Errorf("expected 2 done, got %d", agg.totals.Done)
	}
	if agg.totals.Failed != 1 {
		t.Errorf("expected 1 failed, got %d", agg.totals.Failed)
	}
	if agg.totals.Other != 1 {
		t.Errorf("expected 1 other, got %d", agg.totals.Other)
	}

	// Model grouping: sonnet x2, gpt-4 x1, llama3 x1
	if len(agg.byModel) != 3 {
		t.Errorf("expected 3 model rows, got %d", len(agg.byModel))
	}
	// Sorted descending by sessions: sonnet first
	if agg.byModel[0].Model != "sonnet" {
		t.Errorf("expected sonnet first, got %s", agg.byModel[0].Model)
	}
	if agg.byModel[0].Sessions != 2 {
		t.Errorf("expected sonnet=2, got %d", agg.byModel[0].Sessions)
	}

	// Tool grouping: claude x2, codex x1, ollama x1
	if len(agg.byTool) != 3 {
		t.Errorf("expected 3 tool rows, got %d", len(agg.byTool))
	}
	if agg.byTool[0].Tool != "claude" {
		t.Errorf("expected claude first, got %s", agg.byTool[0].Tool)
	}
	if agg.byTool[0].Sessions != 2 {
		t.Errorf("expected claude=2, got %d", agg.byTool[0].Sessions)
	}
}

func TestAggregateSessionsDuration(t *testing.T) {
	now := time.Now()
	sessions := []protocol.SessionSummary{
		{
			ID: "1", ToolID: "claude", ModelID: "sonnet", State: protocol.StateDone,
			StartedAt: now.Add(-2 * time.Hour), LastEventAt: now.Add(-1 * time.Hour),
		},
	}
	agg := aggregateSessions(sessions)
	if len(agg.byModel) != 1 {
		t.Fatalf("expected 1 model row")
	}
	d := agg.byModel[0].duration
	// Should be approximately 1 hour.
	if d < 59*time.Minute || d > 61*time.Minute {
		t.Errorf("expected ~1h duration, got %v", d)
	}
}

func TestAggregateSessionsNoTokens(t *testing.T) {
	sessions := []protocol.SessionSummary{
		{ID: "1", ToolID: "claude", ModelID: "sonnet", State: protocol.StateDone},
	}
	agg := aggregateSessions(sessions)
	if agg.hasTokens {
		t.Errorf("expected hasTokens=false when no token data")
	}
}

func TestAggregateSessionsSortTieBreak(t *testing.T) {
	// When counts are equal, models should be sorted alphabetically.
	sessions := []protocol.SessionSummary{
		{ID: "1", ToolID: "t", ModelID: "zephyr", State: protocol.StateDone},
		{ID: "2", ToolID: "t", ModelID: "alpaca", State: protocol.StateDone},
	}
	agg := aggregateSessions(sessions)
	if agg.byModel[0].Model != "alpaca" {
		t.Errorf("expected alpaca first on tie, got %s", agg.byModel[0].Model)
	}
}


func TestFormatTokens(t *testing.T) {
	cases := []struct {
		n    int64
		want string
	}{
		{500, "500 tk"},
		{1500, "1K tk"},
		{214000, "214K tk"},
		{1200000, "1.2M tk"},
	}
	for _, c := range cases {
		got := formatTokens(c.n)
		if got != c.want {
			t.Errorf("formatTokens(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

func TestSessionDurationLive(t *testing.T) {
	now := time.Now()
	s := protocol.SessionSummary{
		State:     protocol.StateWorking,
		StartedAt: now.Add(-5 * time.Minute),
		// LastEventAt is set but should be ignored for live sessions.
		LastEventAt: now.Add(-4 * time.Minute),
	}
	d := sessionDuration(s)
	// For working sessions, uses time.Now() as end — should be ~5m.
	if d < 4*time.Minute || d > 6*time.Minute {
		t.Errorf("expected ~5m live duration, got %v", d)
	}
}
