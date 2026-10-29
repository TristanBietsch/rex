package tui

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/hinshun/vt10x"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// multilinePrompt is the prompt from the user's report: a session started
// with it rendered its board row over five screen lines.
const multilinePrompt = "Prompt\n\nI am new\n\nThis repo is a copy"

// requireOneRow fails unless s is a single terminal row exactly w columns
// wide with no control bytes besides SGR escapes.
func requireOneRow(t *testing.T, s string, w int, msgAndArgs ...any) {
	t.Helper()
	require.NotContains(t, s, "\n", msgAndArgs...)
	require.NotContains(t, s, "\r", msgAndArgs...)
	require.NotContains(t, s, "\t", msgAndArgs...)
	require.NotContains(t, s, "\x1b[2J", msgAndArgs...)
	require.Equal(t, w, ansi.StringWidth(s), msgAndArgs...)
}

func TestRenderRow_MultilineTextStaysOneRow(t *testing.T) {
	now := time.Now()
	m := Model{Filter: "all", Focus: FocusBoard, Store: settings.NewStore(), Clock: func() time.Time { return now }}
	m.Sessions = []protocol.SessionSummary{
		{ID: "a", ShortID: "a\nb", Slug: "s\tlug", Title: multilinePrompt, ModelID: "m\x1b[2J",
			State: protocol.StateWorking, LastEventAt: now},
		{ID: "b", ShortID: "bbbb", Slug: "b", Name: "名前\nです", Description: "日本語\r\nの説明\x1b[2J",
			State: protocol.StateNeedsInput, LastEventAt: now},
	}
	for _, w := range []int{40, 80, 225} {
		cols := layoutSlots(boardSlots(m), now, w)
		for _, s := range m.Sessions {
			requireOneRow(t, renderRow(m, s, cols), w, "session %s width %d", s.ID, w)
		}
	}
	row := ansi.Strip(renderRow(m, m.Sessions[0], layoutSlots(boardSlots(m), now, 225)))
	require.Contains(t, row, "Prompt I am new This repo is a copy")
}

// eventEnv wraps v as a daemon event of type typ.
func eventEnv(t *testing.T, typ string, v any) protocol.Envelope {
	t.Helper()
	data, err := json.Marshal(v)
	require.NoError(t, err)
	return protocol.Envelope{Kind: protocol.KindEvent, Type: typ, Data: data}
}

func TestApplyEvent_TextFieldsIngestedAsOneRow(t *testing.T) {
	hostile := "a\nb\tc\x1b[2J"
	const clean = "a b c[2J"
	sum := protocol.SessionSummary{ID: "1", Slug: hostile, Title: hostile, LastLine: hostile,
		Description: hostile, Fleet: hostile, Name: hostile}
	check := func(s protocol.SessionSummary, from string) {
		t.Helper()
		for field, v := range map[string]string{"slug": s.Slug, "title": s.Title, "last_line": s.LastLine,
			"description": s.Description, "fleet": s.Fleet, "name": s.Name} {
			require.Equal(t, clean, v, "%s via %s", field, from)
		}
	}

	var m Model
	m = m.applyEvent(eventEnv(t, protocol.EventSnapshot, protocol.Snapshot{Sessions: []protocol.SessionSummary{sum}}))
	check(m.Sessions[0], "snapshot")

	// The boot handshake's snapshot arrives as a message, not an event.
	booted, _ := Model{Focus: FocusBoot}.Update(snapshotResultMsg{Snap: &protocol.Snapshot{Sessions: []protocol.SessionSummary{sum}}})
	check(booted.(Model).Sessions[0], "boot snapshot")

	m = Model{}.applyEvent(eventEnv(t, protocol.EventSessionAdded, sum))
	check(m.Sessions[0], "session_added")

	m = Model{Sessions: []protocol.SessionSummary{{ID: "1"}}}
	m = m.applyEvent(eventEnv(t, protocol.EventSessionUpdated, protocol.SessionUpdated{SessionID: "1",
		Patch: map[string]any{"slug": hostile, "title": hostile, "last_line": hostile,
			"description": hostile, "fleet": hostile, "name": hostile}}))
	check(m.Sessions[0], "patch")

	m = m.applyEvent(eventEnv(t, protocol.EventError, protocol.ErrorEvent{Code: "x", Message: hostile}))
	require.Equal(t, "daemon: "+clean, m.Err)

	m = m.applyEvent(eventEnv(t, protocol.EventSummarizerHealth, protocol.SummarizerHealth{Reason: "500\n{\"error\":\"x\"}"}))
	require.Equal(t, `500 {"error":"x"}`, m.BackendUnavailableReason)
}

// inputSites are the text inputs drawn on one row: each opens its bar on a
// fresh model, and reads back what was typed. The prompt and wizard texts
// become the agent's initial prompt, so they keep pasted line breaks
// (multiline) and only their drawing is flattened.
var inputSites = []struct {
	name      string
	multiline bool
	open      func(Model) Model
	text      func(Model) string
}{
	{"prompt", true, func(m Model) Model { m.Focus = FocusPrompt; return m },
		func(m Model) string { return m.PromptText }},
	{"command", false, func(m Model) Model { m.Focus = FocusCommand; return m },
		func(m Model) string { return m.CmdText }},
	{"rename", false, func(m Model) Model { m, _ = openRename(m); return m },
		func(m Model) string { return m.Rename.Draft }},
	{"wizard", true, func(m Model) Model {
		m.Focus = FocusWizard
		m.Wizard = &WizardState{Step: wizDescribe, Tools: demoTools()}
		return m
	}, func(m Model) string { return m.Wizard.TaskText }},
	{"attach command", false, func(m Model) Model {
		m.Focus = FocusCommand
		m.Attach = &AttachState{SessionID: m.SelectedID, Term: vt10x.New(vt10x.WithSize(40, 8)), Cols: 40, Rows: 8}
		return m
	}, func(m Model) string { return m.CmdText }},
}

func inputModel() Model {
	m := Model{Filter: "all", Focus: FocusBoard, Width: 80, Height: 24, Store: settings.NewStore(),
		Sessions: []protocol.SessionSummary{{ID: "1", ShortID: "aaaa", Slug: "one", State: protocol.StateWorking}}}
	m.SelectedID = "1"
	return m
}

func pasteKey(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s), Paste: true}
}

func TestInputSites_PasteFlattensToOneLine(t *testing.T) {
	for _, site := range inputSites {
		if site.multiline {
			continue
		}
		m := site.open(inputModel())
		m, _ = updateKey(m, pasteKey(multilinePrompt))
		require.Equal(t, "Prompt I am new This repo is a copy", site.text(m), site.name)

		m = site.open(inputModel())
		m, _ = updateKey(m, pasteKey("a\r\nb\tc\x1b[2J\x00"))
		require.Equal(t, "a b c[2J", site.text(m), site.name)

		m = site.open(inputModel())
		m, _ = updateKey(m, pasteKey("foo\n"))
		m, _ = updateKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("bar")})
		require.Equal(t, "foo bar", site.text(m), site.name)
	}
}

// The λ prompt and the wizard's task text keep a paste's line breaks and
// tabs, so a list or code block reaches the agent intact; escapes and other
// controls still go. They are drawn on one row.
func TestPromptInputs_PasteKeepsLineBreaks(t *testing.T) {
	for _, site := range inputSites {
		if !site.multiline {
			continue
		}
		m := site.open(inputModel())
		m, _ = updateKey(m, pasteKey(multilinePrompt))
		require.Equal(t, multilinePrompt, site.text(m), site.name)

		m = site.open(inputModel())
		m, _ = updateKey(m, pasteKey("a\r\nb\tc\x1b[2J\x00\rd\u202e\u009be\xff"))
		require.Equal(t, "a\nb\tc[2J\nde", site.text(m), site.name)

		m = site.open(inputModel())
		m, _ = updateKey(m, pasteKey("- one\n- two\n"))
		m, _ = updateKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
		require.Equal(t, "- one\n- two\nx", site.text(m), site.name)

		lines := strings.Split(m.View(), "\n")
		require.Len(t, lines, m.Height, site.name)
		require.Equal(t, ansi.Strip(firstLine(renderHelpLine(m, m.Width))), ansi.Strip(lines[m.Height-1]),
			"%s: help bar last", site.name)
		require.Contains(t, ansi.Strip(m.View()), "- one - two x", site.name)
	}
}

// A pasted multi-line prompt reaches NewSession's InitialPrompt intact from
// both the quick-spawn prompt and the wizard.
func TestPromptInputs_MultilineReachesInitialPrompt(t *testing.T) {
	const pasted = "Fix these:\n\n- a\n- b\n\n```go\nfunc f() {}\n```"
	store := settings.NewStore()
	require.NoError(t, store.Set("default_spawn_tool", "claude"))
	require.NoError(t, store.Set("default_spawn_model", "opus"))
	for _, site := range inputSites {
		if !site.multiline {
			continue
		}
		rec := &recordingClient{}
		m := site.open(inputModel())
		m.Store, m.spawner = store, rec
		m, _ = updateKey(m, pasteKey(pasted))
		m, cmd := updateKey(m, key("enter"))
		require.Equal(t, FocusBoard, m.Focus, site.name)
		require.NotNil(t, cmd, site.name)
		require.Nil(t, cmd(), site.name)
		require.NotNil(t, rec.last, site.name)
		require.Equal(t, pasted, rec.last.InitialPrompt, site.name)
		require.Equal(t, m.PendingSelectSlug, rec.last.Slug, site.name)
		require.NotContains(t, rec.last.Slug, "\n", site.name)
	}
}

func TestRenderDeleteConfirm_OneRow(t *testing.T) {
	m := Model{PendingDeleteID: "1",
		Sessions: []protocol.SessionSummary{{ID: "1", Slug: "a\nb\tc\x1b[2J\u202e"}}}
	got := renderDeleteConfirm(m, 200)
	requireOneRow(t, got, 200)
	require.NotContains(t, got, "\u202e")
	require.Contains(t, ansi.Strip(got), "delete session a b c[2J?")
}

func TestTruncate_SanitizesToOneRow(t *testing.T) {
	require.Equal(t, "a b c[2J", truncate("a\nb\tc\x1b[2J", 20))
	require.Equal(t, "日本…", truncate("日本語\nテキスト", 6))
}

func TestRenderAnimFrame_OneRowExactColumns(t *testing.T) {
	start := time.Now()
	for _, effect := range []string{"typewriter", "decode", "wipe"} {
		a := DescAnim{From: "古い説明\nold", To: "日本語のテキスト\tand 😀 more\x1b[2J",
			Effect: effect, StartedAt: start, Duration: 300 * time.Millisecond}
		for _, w := range []int{1, 7, 10, 31} {
			for _, p := range []float64{0, 0.3, 0.5, 0.9, 1} {
				at := start.Add(time.Duration(float64(a.Duration) * p))
				requireOneRow(t, renderAnimFrame(a, w, at), w, "%s width %d p %v", effect, w, p)
			}
		}
	}
	end := start.Add(time.Second)
	got := renderAnimFrame(DescAnim{To: "a\nb", Effect: "typewriter", StartedAt: start, Duration: time.Millisecond}, 6, end)
	require.Equal(t, "a b   ", got)
}
