package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/tristanbietsch/rex/internal/protocol"
	"github.com/tristanbietsch/rex/internal/registry"
	"github.com/tristanbietsch/rex/internal/settings"
)

func demoSessions(now time.Time) []protocol.SessionSummary {
	return []protocol.SessionSummary{
		{ID: "1", ShortID: "7d4f", ToolID: "claude", ModelID: "opus", Effort: "high",
			Slug: "dark-mode", LastLine: "system theme vs explicit toggle — your call",
			State: protocol.StateNeedsInput, LastEventAt: now.Add(-4 * time.Minute)},
		{ID: "2", ShortID: "b203", ToolID: "codex", ModelID: "gpt-5", Effort: "medium",
			Slug: "release-notes", LastLine: "draft ready — which feature leads?",
			State: protocol.StateNeedsInput, LastEventAt: now.Add(-11 * time.Minute)},
		{ID: "3", ShortID: "3c8a", ToolID: "claude", ModelID: "sonnet", Effort: "default",
			Slug: "perf-audit", LastLine: "events_org_ts index live — p95 38ms",
			State: protocol.StateWorking, LastEventAt: now.Add(-7 * time.Minute)},
		{ID: "4", ShortID: "9e51", ToolID: "codex", ModelID: "gpt-5-codex", Effort: "med",
			Slug: "payment-migration", LastLine: "porting billing to the new processor — 12/14",
			State: protocol.StateWorking, LastEventAt: now.Add(-2 * time.Minute)},
		{ID: "5", ShortID: "2b7e", ToolID: "claude", ModelID: "haiku", Effort: "default",
			Slug: "test-coverage", LastLine: "billing/ from 61% → 92% — PR #408 merged",
			State: protocol.StateDone, LastEventAt: now.Add(-9 * time.Minute)},
	}
}

func demoTools() []registry.Tool {
	return []registry.Tool{
		{ID: "echo", Name: "Echo (test)", Category: "self_hosted"},
		{ID: "claude", Name: "Claude Code", Category: "paid",
			Models: []registry.Model{{ID: "opus", Name: "Opus 4.7", Effort: &registry.Effort{Options: []string{"minimal", "default", "high", "max"}, Default: "default"}},
				{ID: "sonnet", Name: "Sonnet 4.6"}, {ID: "haiku", Name: "Haiku 4.5"}}},
		{ID: "codex", Name: "OpenAI Codex", Category: "paid"},
		{ID: "gemini", Name: "Gemini CLI", Category: "paid"},
		{ID: "ollama", Name: "Ollama (local)", Category: "self_hosted"},
	}
}

// TestBoardSnapshot prints View() for each focus mode at a wide terminal.
func TestBoardSnapshot(t *testing.T) {
	sessions := demoSessions(time.Now())
	tools := demoTools()
	base := Model{
		Sessions:   sessions,
		Filter:     "all",
		SelectedID: "1",
		Width:      200, // intentionally wide — verifies the cap
		Height:     40,
	}

	cases := []struct {
		name  string
		setup func(m Model) Model
	}{
		{"board-default", func(m Model) Model { m.Focus = FocusBoard; return m }},
		{"wiz-1-provider", func(m Model) Model {
			m.Focus = FocusWizard
			m.Wizard = &WizardState{Step: wizProvider, Tools: tools}
			return m
		}},
		{"wiz-2-effort", func(m Model) Model {
			m.Focus = FocusWizard
			m.Wizard = &WizardState{Step: wizEffort, Tools: tools, ToolIdx: 1, EffortIdx: 1}
			return m
		}},
		{"wiz-3-describe", func(m Model) Model {
			m.Focus = FocusWizard
			m.Wizard = &WizardState{Step: wizDescribe, Tools: tools, ToolIdx: 1, TaskText: "fix auth bug"}
			return m
		}},
	}

	for _, c := range cases {
		m := c.setup(base)
		out := m.View()
		if out == "" {
			t.Fatalf("%s: empty view", c.name)
		}
		if testing.Verbose() {
			fmt.Printf("\n══ %s ══\n%s\n", c.name, out)
		}
	}
}

func TestBoardRendersDescriptionAnimating(t *testing.T) {
	m := Model{
		Sessions: []protocol.SessionSummary{{
			ID: "sess-x", ShortID: "abcd", Slug: "test", State: protocol.StateWorking,
			Description: "running pnpm test", LastLine: "raw garbage line",
		}},
		Store: settings.NewStore(),
		DescAnim: map[string]DescAnim{
			"sess-x": {
				From: "", To: "running pnpm test", Effect: "typewriter",
				StartedAt: time.Now().Add(-150 * time.Millisecond),
				Duration:  300 * time.Millisecond,
			},
		},
	}
	out := renderBoard(m, 120, 10)
	// Halfway through typewriter on a 17-rune target → expect ~8 runes visible.
	if !strings.Contains(out, "running p") && !strings.Contains(out, "running pn") {
		t.Fatalf("expected partial typewriter output, got:\n%s", out)
	}
	// Raw LastLine must NOT appear — the renderer should prefer Description.
	if strings.Contains(out, "raw garbage line") {
		t.Fatalf("renderer fell back to LastLine when Description is set")
	}
}
