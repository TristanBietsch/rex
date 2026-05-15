package tui

import (
	"fmt"
	"testing"
	"time"

	"github.com/tristanbietsch/rex/internal/protocol"
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
		{ID: "5", ShortID: "4f6d", ToolID: "gemini", ModelID: "2.5-pro",
			Slug: "onboarding-copy", LastLine: "rewriting empty-state copy across 6 screens",
			State: protocol.StateWorking, LastEventAt: now.Add(-1 * time.Minute)},
		{ID: "6", ShortID: "c14a", ToolID: "ollama", ModelID: "llama3.1",
			Slug: "load-test", LastLine: "k6 against the launch traffic profile…",
			State: protocol.StateWorking, LastEventAt: now.Add(-3 * time.Minute)},
		{ID: "7", ShortID: "2b7e", ToolID: "claude", ModelID: "haiku", Effort: "default",
			Slug: "test-coverage", LastLine: "billing/ from 61% → 92% — PR #408 merged",
			State: protocol.StateDone, LastEventAt: now.Add(-9 * time.Minute)},
	}
}

// TestBoardSnapshot prints the View() output for each focus mode.
// Run with -v to inspect; passes if every state produces a non-empty view.
func TestBoardSnapshot(t *testing.T) {
	sessions := demoSessions(time.Now())
	base := Model{
		Sessions:   sessions,
		Filter:     "all",
		SelectedID: "1",
		Width:      120,
		Height:     36,
	}

	cases := []struct {
		name  string
		setup func(m Model) Model
	}{
		{"board-default", func(m Model) Model { m.Focus = FocusBoard; return m }},
		{"board-prompt-focused", func(m Model) Model {
			m.Focus = FocusPrompt
			m.PromptText = "rewrite the marketing page hero"
			return m
		}},
		{"board-command-mode", func(m Model) Model {
			m.Focus = FocusCommand
			m.CmdText = "rm 9e51"
			m.SelectedID = "4"
			return m
		}},
		{"board-filter-claude", func(m Model) Model {
			m.Focus = FocusBoard
			m.Filter = "claude"
			return m
		}},
		{"confirm-quit", func(m Model) Model { m.Focus = FocusConfirmQuit; return m }},
		{"help", func(m Model) Model { m.Focus = FocusHelp; return m }},
		{"slash", func(m Model) Model {
			m.Focus = FocusSlash
			m.Slash = &SlashState{Query: "fi", CursorIdx: 0}
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

// TestBoardSmallScreen verifies the board still produces output at minimum widths.
func TestBoardSmallScreen(t *testing.T) {
	m := Model{
		Focus:    FocusBoard,
		Sessions: demoSessions(time.Now()),
		Filter:   "all",
		Width:    60,
		Height:   18,
	}
	out := m.View()
	if out == "" {
		t.Fatal("empty view at 60x18")
	}
}
