package tui

import (
	"context"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/runtime/daemonctl"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// commandModel is a board with two sessions and a throwaway settings file,
// ready to take `:` commands. Returned commands are never run: :reload's
// would reach a real daemon unless the test sets m.reloader.
func commandModel(t *testing.T) Model {
	t.Helper()
	m := boardModel(
		protocol.SessionSummary{ID: "id-alpha", ShortID: "aaaa", Slug: "alpha", State: protocol.StateNeedsInput},
		protocol.SessionSummary{ID: "id-beta", ShortID: "bbbb", Slug: "beta", State: protocol.StateFailed},
	)
	m.Store = settings.NewStore()
	m.StorePath = filepath.Join(t.TempDir(), "config.yaml")
	m.retirer = &fakeRetirer{}
	return m
}

// typeCommand drives the real key path through Update: ":" opens the bar,
// the line is typed rune by rune, enter runs it. Returned commands are
// dropped, not executed.
func typeCommand(m Model, line string) Model {
	m = sendKey(m, key(":"))
	for _, r := range line {
		m = sendKey(m, key(string(r)))
	}
	return sendKey(m, key("enter"))
}

func sendKey(m Model, k tea.KeyMsg) Model {
	next, _ := m.Update(k)
	return next.(Model)
}

// Every `:` verb must leave the TUI somewhere esc gets back to the board
// (or quit). `:new` used to set FocusWizard without opening the wizard: with
// Wizard nil every key, esc included, was dropped and the TUI soft-locked
// (backlog P0-07).
func TestCommand_EveryVerbEscapesToBoard(t *testing.T) {
	cases := []struct {
		line string
		quit bool
	}{
		{line: "q"},
		{line: "quit"},
		{line: "q!", quit: true},
		{line: "bg", quit: true},
		{line: "detach", quit: true},
		{line: "help"},
		{line: "settings"},
		{line: "reload"},
		{line: "filter claude"},
		{line: "rm aaaa"},
		{line: "rm"},
		{line: "rename aaaa new-slug"},
		{line: "rename"},
		{line: "name"},
		{line: "name my label"},
		{line: "done"},
		{line: "undone"},
		{line: "complete"},
		{line: "complete nope"},
		{line: "new"},
		{line: "fail"},
		{line: "fails"},
		{line: "failed"},
		{line: "stats"},
		{line: "usage"},
		{line: "no-such-verb"},
	}
	for _, tc := range cases {
		t.Run(tc.line, func(t *testing.T) {
			m := typeCommand(commandModel(t), tc.line)
			if tc.quit {
				require.True(t, m.Quitting, "%q quits", tc.line)
				return
			}
			m = sendKey(m, key("esc"))
			require.Equal(t, FocusBoard, m.Focus, ":%s then esc returns to the board", tc.line)
		})
	}
}

// :new opens the same wizard as `n` (new_agent_page off).
func TestCommand_NewOpensWizard(t *testing.T) {
	m := typeCommand(wizardFlag(t, commandModel(t)), "new")
	require.Equal(t, FocusWizard, m.Focus)
	require.NotNil(t, m.Wizard)
	require.NotEmpty(t, m.Wizard.Tools)
}

// The invariant behind P0-07: a focus whose component state is missing
// routes back to the board instead of silently dropping every key. The key
// that hit the broken focus is then handled by the board.
func TestUpdateKey_MissingFocusStateFallsBackToBoard(t *testing.T) {
	for name, f := range map[string]Focus{
		"wizard": FocusWizard, "settings": FocusSettings, "attach": FocusAttach,
		"fail": FocusFail, "rename": FocusRename,
	} {
		t.Run(name, func(t *testing.T) {
			m := commandModel(t)
			m.Focus = f
			m = sendKey(m, key("j"))
			require.Equal(t, FocusBoard, m.Focus)
			require.Equal(t, "id-beta", m.SelectedID, "j reached the board")
		})
	}
}

// FU-01/FU-11: :reload sends a Reload intent to the daemon on the TUI's own
// socket (not the default one, and never a signal to a process found by
// name), and a failure shows on the board instead of vanishing.
func TestCommand_ReloadUsesTheTUISocket(t *testing.T) {
	m := commandModel(t)
	m.Socket = "/tmp/rex-test/custom.sock"
	var got []string
	m.reloader = func(_ context.Context, socket string) error {
		got = append(got, socket)
		return nil
	}
	_, cmd := executeCommand(m, "reload")
	require.NotNil(t, cmd)
	for _, msg := range collectMsgs(t, cmd) {
		_, isErr := msg.(CmdErrMsg)
		require.False(t, isErr, "a successful reload reports no error")
	}
	require.Equal(t, []string{"/tmp/rex-test/custom.sock"}, got)

	m.reloader = func(context.Context, string) error { return daemonctl.ErrNotRunning }
	_, cmd = executeCommand(m, "reload")
	var failed *CmdErrMsg
	for _, msg := range collectMsgs(t, cmd) {
		if e, ok := msg.(CmdErrMsg); ok {
			failed = &e
		}
	}
	require.NotNil(t, failed, "a failed reload must be reported")
	require.Equal(t, "reload", failed.Op)
	require.ErrorIs(t, failed.Err, daemonctl.ErrNotRunning)
}
