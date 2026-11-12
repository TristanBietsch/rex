package tui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

const (
	wsA = "7f3b89a2"
	wsB = "0c11d2e3"
)

func wsSessions() []protocol.SessionSummary {
	return []protocol.SessionSummary{
		{ID: "1", ShortID: "aa11", ToolID: "claude", ModelID: "opus", Slug: "in-a", State: protocol.StateWorking, WorkspaceID: wsA},
		{ID: "2", ShortID: "bb22", ToolID: "claude", ModelID: "opus", Slug: "in-b", State: protocol.StateWorking, WorkspaceID: wsB},
		{ID: "3", ShortID: "cc33", ToolID: "claude", ModelID: "opus", Slug: "loose", State: protocol.StateNeedsInput},
		{ID: "4", ShortID: "dd44", ToolID: "claude", ModelID: "opus", Slug: "a-done", State: protocol.StateDone, WorkspaceID: wsA},
	}
}

func wsScopeA() *WorkspaceScope {
	return &WorkspaceScope{ID: wsA, Name: "api", Color: "#0072B2", Root: "/work/api"}
}

func wsModel(t *testing.T, on bool) Model {
	t.Helper()
	m := layoutModel(goldenNow, 120, wsSessions()...)
	m.Detailed = false
	m.SelectedID = "1"
	m.WS = wsScopeA()
	m.wsTagging = true
	require.NoError(t, m.Store.Set("workspaces", on))
	return m
}

func TestWorkspaceBoard_ShowsOnlyItsSessions(t *testing.T) {
	m := wsModel(t, true)
	f := rowFilterOf(m)
	require.Equal(t, wsA, f.workspace)
	require.False(t, f.rowsActive(), "the fixed scope is not a user filter")
	require.Equal(t, []string{"1", "4"}, ids(orderedSessions(m)))
	require.Equal(t, []string{"1", "4"}, ids(scopedSessions(m)))

	out := ansi.Strip(renderBoard(m, 120, 0))
	require.Contains(t, out, "in-a")
	require.NotContains(t, out, "in-b")
	require.NotContains(t, out, "loose")
}

func TestWorkspaceBoard_HeaderCountsAreScoped(t *testing.T) {
	m := wsModel(t, true)
	hdr := ansi.Strip(renderHeader(m, 120))
	require.Contains(t, hdr, "0 ready · 1 working · 1 done")
	m.WS = nil
	hdr = ansi.Strip(renderHeader(m, 120))
	require.Contains(t, hdr, "1 ready · 2 working · 1 done")
}

func TestWorkspaceBoard_HeaderChip(t *testing.T) {
	m := wsModel(t, true)
	first := strings.Split(ansi.Strip(renderHeader(m, 120)), "\n")[0]
	require.True(t, strings.HasPrefix(strings.TrimSpace(first), "["+wsA+" api]"), first)
	require.Less(t, strings.Index(first, "["+wsA), strings.Index(first, "REX"), "the chip is top-left")

	withProfile(termenv.TrueColor, func() {
		styled := strings.Split(renderHeader(m, 120), "\n")[0]
		require.Contains(t, styled, ";178m", "workspace color #0072B2 on the chip (blue channel 178)")
	})
	withProfile(termenv.Ascii, func() {
		require.NotContains(t, renderHeader(m, 120), "\x1b[", "NO_COLOR: text only")
	})

	m.Width = 40
	m.Store = settingsWith(t, map[string]any{"workspaces": true, "mobile_layout": "always"})
	first = ansi.Strip(strings.Split(renderHeader(m, 40), "\n")[0])
	require.Contains(t, first, "["+wsA+"]", "narrow layouts drop the name")
}

func settingsWith(t *testing.T, kv map[string]any) *settings.Store {
	t.Helper()
	st := settings.NewStore()
	for k, v := range kv {
		require.NoError(t, st.Set(k, v))
	}
	return st
}

func TestWorkspaceBoard_NoChipOnOrdinaryBoard(t *testing.T) {
	m := wsModel(t, true)
	m.WS = nil
	require.Empty(t, renderWorkspaceChip(m))
	require.NotContains(t, ansi.Strip(renderHeader(m, 120)), "[")
}

func TestWorkspaceBoard_FeatureOffIsInert(t *testing.T) {
	m := wsModel(t, false)
	require.Nil(t, m.wsScope())
	require.Empty(t, rowFilterOf(m).workspace)
	require.Equal(t, 4, len(scopedSessions(m)))
	require.Empty(t, renderWorkspaceChip(m))
	require.False(t, borderEnabled(m))
	require.NotEqual(t, "/work/api", spawnCWD(m), "the wizard starts where the TUI was started")
}

func TestWorkspaceBoard_OldDaemonShowsEverythingWithOneNotice(t *testing.T) {
	m := wsModel(t, true)
	m.wsTagging = false
	var untagged []protocol.SessionSummary
	for _, s := range wsSessions() {
		s.WorkspaceID = ""
		untagged = append(untagged, s)
	}
	snap := protocol.Snapshot{Sessions: untagged}
	m = adoptWorkspaceTagging(m, &snap)
	m.Sessions = untagged
	require.Equal(t, wsOldDaemonNotice, m.Notice)
	require.Empty(t, rowFilterOf(m).workspace)
	require.Len(t, orderedSessions(m), 4, "every session shows")

	m.Notice = ""
	m = adoptWorkspaceTagging(m, &snap)
	require.Empty(t, m.Notice, "one notice, not one per snapshot")
}

func TestWorkspaceBoard_DaemonSaysItTags(t *testing.T) {
	m := wsModel(t, true)
	m.wsTagging = false
	// A new daemon with no tagged session yet still scopes the board.
	m = adoptWorkspaceTagging(m, &protocol.Snapshot{WorkspaceTagging: true, Sessions: []protocol.SessionSummary{wsSessions()[2]}})
	require.True(t, m.wsTagging)
	require.Empty(t, m.Notice)

	// One tagged session is proof too.
	m.wsTagging = false
	m = adoptWorkspaceTagging(m, &protocol.Snapshot{Sessions: wsSessions()})
	require.True(t, m.wsTagging)
}

func TestWorkspaceBoard_TaggedAddFlipsAnUnknownBoard(t *testing.T) {
	m := wsModel(t, true)
	m.wsTagging = false
	raw, err := json.Marshal(wsSessions()[0])
	require.NoError(t, err)
	m = m.applyEvent(protocol.Envelope{Type: protocol.EventSessionAdded, Data: raw})
	require.True(t, m.wsTagging)
}

func TestWorkspaceBoard_WizardAndQuickSpawnStartAtTheRoot(t *testing.T) {
	m := wsModel(t, true)
	require.Equal(t, "/work/api", spawnCWD(m))
	m.WS = nil
	cwd := spawnCWD(m)
	require.NotEqual(t, "/work/api", cwd)

	m = wsModel(t, true)
	fake := &recordingClient{}
	m.Store = settingsWith(t, map[string]any{"workspaces": true, "default_spawn_tool": "claude", "default_spawn_model": "opus"})
	cmd := spawnSessionIn(spawnCWD(m), fake, m.Store, nil, "fix it", 0, 0)
	require.Nil(t, cmd())
	require.Equal(t, "/work/api", fake.last.CWD)
}

func TestWorkspaceBoard_PruneOnlyTouchesThisWorkspace(t *testing.T) {
	m := wsModel(t, true)
	m.Sessions = append(m.Sessions, protocol.SessionSummary{ID: "5", ShortID: "ee55", Slug: "b-done", State: protocol.StateDone, WorkspaceID: wsB})
	m, _ = openPrune(m)
	require.Equal(t, []string{"4"}, m.PendingPrune)
}

func TestWorkspaceBoard_BorderUsesWorkspaceColor(t *testing.T) {
	m := wsModel(t, true)
	require.True(t, borderEnabled(m), "scoped boards default the border on")
	edge := borderEdge(m, true)
	require.Equal(t, "#0072b2", edge.ID)
	withProfile(termenv.TrueColor, func() {
		require.Equal(t, lipgloss.Color("#0072b2"), borderColorOf(m))
	})

	// An explicit window_border=false wins.
	require.NoError(t, m.Store.Set("window_border", false))
	require.False(t, borderEnabled(m))
	// ...and an explicit color wins over the workspace color.
	require.NoError(t, m.Store.Set("window_border", true))
	require.NoError(t, m.Store.Set("window_border_color", "tangerine"))
	require.Equal(t, "tangerine", borderEdge(m, true).ID)
	// :border on/off for this run wins over everything.
	off := false
	m.borderRunOn = &off
	require.False(t, borderEnabled(m))
}

func TestWorkspaceBoard_BadColorFallsBack(t *testing.T) {
	m := wsModel(t, true)
	m.WS.Color = "red; rm -rf"
	_, ok := wsEdge(m.WS)
	require.False(t, ok)
	require.NotEqual(t, "#0072b2", borderEdge(m, true).ID)
	require.Contains(t, ansi.Strip(renderWorkspaceChip(m)), wsA, "the chip still shows its text")
}

func TestWorkspaceBoard_ChipNameIsSanitizedAndCut(t *testing.T) {
	m := wsModel(t, true)
	m.WS.Name = "a\x1b[31mb" + strings.Repeat("x", 80)
	chip := renderWorkspaceChip(m)
	require.NotContains(t, ansi.Strip(chip), "\x1b")
	require.LessOrEqual(t, ansi.StringWidth(chip), len(wsA)+maxChipNameCells+3)
}
