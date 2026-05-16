package cli

import (
	"testing"
	"time"

	"github.com/tristanbietsch/rex/internal/protocol"
	"github.com/tristanbietsch/rex/internal/state"
)

func makeSession(id string, toolID, modelID string, stateVal protocol.State, startedAt, lastEventAt time.Time) protocol.SessionSummary {
	shortID := id
	if len(shortID) > 4 {
		shortID = shortID[:4]
	}
	return protocol.SessionSummary{
		ID:          id,
		ShortID:     shortID,
		ToolID:      toolID,
		ModelID:     modelID,
		State:       stateVal,
		StartedAt:   startedAt,
		LastEventAt: lastEventAt,
		Slug:        "test-slug-" + id,
	}
}

func localMidnight(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.Local)
}

// --- digestRange.contains ---

func TestDigestRangeContains_StartedAtInRange(t *testing.T) {
	today := localMidnight(2026, 5, 15)
	dr := digestRange{start: today, end: today.AddDate(0, 0, 1)}

	s := makeSession("aaa", "claude", "sonnet", protocol.StateDone,
		today.Add(2*time.Hour),          // StartedAt in range
		today.Add(-25*time.Hour),        // LastEventAt outside range (yesterday)
	)
	if !dr.contains(s) {
		t.Error("expected session with StartedAt in range to be included")
	}
}

func TestDigestRangeContains_LastEventAtInRange(t *testing.T) {
	today := localMidnight(2026, 5, 15)
	dr := digestRange{start: today, end: today.AddDate(0, 0, 1)}

	s := makeSession("bbb", "claude", "sonnet", protocol.StateDone,
		today.Add(-25*time.Hour),  // StartedAt yesterday
		today.Add(3*time.Hour),    // LastEventAt today
	)
	if !dr.contains(s) {
		t.Error("expected session with LastEventAt in range to be included")
	}
}

func TestDigestRangeContains_NeitherInRange(t *testing.T) {
	today := localMidnight(2026, 5, 15)
	dr := digestRange{start: today, end: today.AddDate(0, 0, 1)}

	s := makeSession("ccc", "claude", "sonnet", protocol.StateDone,
		today.Add(-48*time.Hour),  // two days ago
		today.Add(-47*time.Hour),
	)
	if dr.contains(s) {
		t.Error("expected session entirely outside range to be excluded")
	}
}

func TestDigestRangeContains_ZeroLastEventAt(t *testing.T) {
	today := localMidnight(2026, 5, 15)
	dr := digestRange{start: today, end: today.AddDate(0, 0, 1)}

	s := makeSession("ddd", "claude", "sonnet", protocol.StateDone,
		today.Add(time.Hour), // StartedAt today
		time.Time{},          // zero LastEventAt
	)
	if !dr.contains(s) {
		t.Error("expected session with zero LastEventAt but StartedAt in range to be included")
	}
}

// --- buildDigestOutput ---

func TestBuildDigestOutput_Counts(t *testing.T) {
	today := localMidnight(2026, 5, 15)
	sessions := []protocol.SessionSummary{
		makeSession("a1", "claude", "sonnet", protocol.StateDone, today.Add(time.Hour), today.Add(2*time.Hour)),
		makeSession("a2", "claude", "opus", protocol.StateFailed, today.Add(time.Hour), today.Add(90*time.Minute)),
		makeSession("a3", "codex", "gpt-5", protocol.StateWorking, today.Add(time.Hour), today.Add(100*time.Minute)),
		makeSession("a4", "claude", "sonnet", protocol.StateNeedsInput, today.Add(time.Hour), today.Add(70*time.Minute)),
	}

	out := buildDigestOutput(sessions, today)

	if out.Totals.Count != 4 {
		t.Errorf("expected 4 total, got %d", out.Totals.Count)
	}
	if out.Totals.Done != 1 {
		t.Errorf("expected 1 done, got %d", out.Totals.Done)
	}
	if out.Totals.Failed != 1 {
		t.Errorf("expected 1 failed, got %d", out.Totals.Failed)
	}
	if out.Totals.Working != 1 {
		t.Errorf("expected 1 working, got %d", out.Totals.Working)
	}
	if out.Totals.NeedsInput != 1 {
		t.Errorf("expected 1 needs_input, got %d", out.Totals.NeedsInput)
	}
	if len(out.ByToolJSON) != 2 {
		t.Errorf("expected 2 tools, got %d", len(out.ByToolJSON))
	}
	// claude appears 3 times, should be first
	if out.ByToolJSON[0].Key != "claude" {
		t.Errorf("expected claude first in by_tool, got %s", out.ByToolJSON[0].Key)
	}
	if out.ByToolJSON[0].Count != 3 {
		t.Errorf("expected claude count 3, got %d", out.ByToolJSON[0].Count)
	}
}

func TestBuildDigestOutput_ByModel(t *testing.T) {
	today := localMidnight(2026, 5, 15)
	sessions := []protocol.SessionSummary{
		makeSession("b1", "claude", "sonnet", protocol.StateDone, today.Add(time.Hour), today.Add(2*time.Hour)),
		makeSession("b2", "claude", "sonnet", protocol.StateDone, today.Add(time.Hour), today.Add(3*time.Hour)),
		makeSession("b3", "claude", "opus", protocol.StateDone, today.Add(time.Hour), today.Add(90*time.Minute)),
	}

	out := buildDigestOutput(sessions, today)

	if len(out.ByModelJSON) != 2 {
		t.Errorf("expected 2 models, got %d", len(out.ByModelJSON))
	}
	if out.ByModelJSON[0].Key != "sonnet" {
		t.Errorf("expected sonnet first, got %s", out.ByModelJSON[0].Key)
	}
}

// --- mergeSessionSummaries ---

func TestMergeSessionSummaries_DaemonWins(t *testing.T) {
	live := []protocol.SessionSummary{
		{ID: "id-1", ShortID: "id-1", State: protocol.StateWorking, Slug: "live-slug"},
	}
	disk := []*state.Session{
		{ID: "id-1", ShortID: "id-1", State: protocol.StateDone, Slug: "disk-slug"},  // same ID
		{ID: "id-2", ShortID: "id-2", State: protocol.StateDone, Slug: "disk-only"},  // only on disk
	}

	merged := mergeSessionSummaries(live, disk)
	if len(merged) != 2 {
		t.Fatalf("expected 2 sessions after merge, got %d", len(merged))
	}
	// id-1 should be the live version
	var found1 *protocol.SessionSummary
	for i := range merged {
		if merged[i].ID == "id-1" {
			found1 = &merged[i]
		}
	}
	if found1 == nil {
		t.Fatal("id-1 not found after merge")
	}
	if found1.State != protocol.StateWorking {
		t.Errorf("expected live state (working) for id-1, got %s", found1.State)
	}
	if found1.Slug != "live-slug" {
		t.Errorf("expected live slug for id-1, got %s", found1.Slug)
	}
}

func TestMergeSessionSummaries_NilDisk(t *testing.T) {
	live := []protocol.SessionSummary{
		{ID: "x", ShortID: "x", State: protocol.StateDone},
	}
	merged := mergeSessionSummaries(live, nil)
	if len(merged) != 1 {
		t.Errorf("expected 1 session, got %d", len(merged))
	}
}

// --- parseDigestRange ---

func TestParseDigestRange_Default(t *testing.T) {
	dr, err := parseDigestRange("", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	if !dr.start.Equal(today) {
		t.Errorf("expected start = today midnight, got %v", dr.start)
	}
	if !dr.end.Equal(today.AddDate(0, 0, 1)) {
		t.Errorf("expected end = tomorrow midnight, got %v", dr.end)
	}
}

func TestParseDigestRange_SpecificDate(t *testing.T) {
	dr, err := parseDigestRange("2026-01-20", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := time.Date(2026, 1, 20, 0, 0, 0, 0, time.Local)
	if !dr.start.Equal(want) {
		t.Errorf("expected start 2026-01-20, got %v", dr.start)
	}
}

func TestParseDigestRange_Yesterday(t *testing.T) {
	dr, err := parseDigestRange("yesterday", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	now := time.Now()
	yesterday := time.Date(now.Year(), now.Month(), now.Day()-1, 0, 0, 0, 0, now.Location())
	if !dr.start.Equal(yesterday) {
		t.Errorf("expected start = yesterday, got %v", dr.start)
	}
}

func TestParseDigestRange_Since7d(t *testing.T) {
	dr, err := parseDigestRange("", "7d")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	wantStart := today.AddDate(0, 0, -6)
	if !dr.start.Equal(wantStart) {
		t.Errorf("expected start = %v, got %v", wantStart, dr.start)
	}
}

func TestParseDigestRange_MutuallyExclusive(t *testing.T) {
	_, err := parseDigestRange("2026-01-01", "7d")
	if err == nil {
		t.Error("expected error for --date + --since combined")
	}
}

func TestParseDigestRange_InvalidDate(t *testing.T) {
	_, err := parseDigestRange("not-a-date", "")
	if err == nil {
		t.Error("expected error for invalid date")
	}
}

func TestParseDigestRange_InvalidSince(t *testing.T) {
	_, err := parseDigestRange("", "abc")
	if err == nil {
		t.Error("expected error for invalid --since")
	}
}

// --- sessionActiveDuration ---

func TestSessionActiveDuration_DoneSession(t *testing.T) {
	start := time.Now().Add(-2 * time.Hour)
	end := time.Now().Add(-1 * time.Hour)
	s := makeSession("x", "claude", "sonnet", protocol.StateDone, start, end)
	d := sessionActiveDuration(s)
	want := end.Sub(start)
	if d != want {
		t.Errorf("expected %v, got %v", want, d)
	}
}

func TestSessionActiveDuration_ZeroLastEvent(t *testing.T) {
	start := time.Now().Add(-time.Hour)
	s := makeSession("x", "claude", "sonnet", protocol.StateDone, start, time.Time{})
	d := sessionActiveDuration(s)
	// end = start when LastEventAt is zero, so duration is 0
	if d != 0 {
		t.Errorf("expected 0, got %v", d)
	}
}

func TestSessionActiveDuration_LiveSession(t *testing.T) {
	start := time.Now().Add(-30 * time.Minute)
	s := makeSession("x", "claude", "sonnet", protocol.StateWorking, start, time.Time{})
	d := sessionActiveDuration(s)
	// Should be approximately 30 minutes (use Now() - StartedAt for live)
	if d < 29*time.Minute || d > 31*time.Minute {
		t.Errorf("expected ~30m, got %v", d)
	}
}

// --- formatDuration ---

func TestFormatDuration(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "< 1m"},
		{5 * time.Minute, "5m"},
		{90 * time.Minute, "1h 30m"},
		{2*time.Hour + 24*time.Minute, "2h 24m"},
	}
	for _, c := range cases {
		got := formatDuration(c.d)
		if got != c.want {
			t.Errorf("formatDuration(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}
