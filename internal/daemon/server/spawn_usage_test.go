package server

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/daemon/adapter"
)

func TestBuildArgv_StatusLineIsAddedToClaudeSettingsOnly(t *testing.T) {
	reg, err := registry.Load("")
	require.NoError(t, err)
	claude, model, ok := reg.FindModel("claude", "opus")
	require.True(t, ok)

	const cmd = "/x/rex-daemon statusline-tap --session sid"
	got := buildArgv(spawnSpec{Tool: claude, Model: model, StatusLine: cmd})
	require.Equal(t, adapter.ClaudeSettingsArgs(cmd), got[len(got)-2:], "the settings arg stays last")
	require.Contains(t, got[len(got)-1], `"statusLine"`)

	got = buildArgv(spawnSpec{Tool: claude, Model: model})
	require.NotContains(t, got[len(got)-1], "statusLine", "no tap command: Claude's footer is untouched")
}

func TestStatusLineFor(t *testing.T) {
	reg, err := registry.Load("")
	require.NoError(t, err)
	claude, _, _ := reg.FindModel("claude", "opus")
	codex, _, _ := reg.FindModel("codex", "gpt-5.5")

	var gotID string
	srv := &Server{cfg: Config{UsageTap: func(id string) string { gotID = id; return "tap" }}}
	require.Equal(t, "tap", srv.statusLineFor(claude, "s1"))
	require.Equal(t, "s1", gotID)
	require.Empty(t, srv.statusLineFor(codex, "s1"), "only Claude has a status line")

	require.Empty(t, (&Server{}).statusLineFor(claude, "s1"), "no UsageTap: none")
}
