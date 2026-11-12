package settings

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsTrustSetting(t *testing.T) {
	require.True(t, IsTrustSetting("skip_permissions"))
	require.True(t, IsTrustSetting("auto_trust_dirs"))
	require.False(t, IsTrustSetting("usage"))
	for _, id := range TrustSettingIDs {
		_, ok := Find(id)
		require.True(t, ok, id)
	}
}

// Every trust setting is denied to a workspace .rex/config.lua: a repo's
// file can never grant or widen trust, whoever trusted the workspace.
func TestWorkspaceDenyIDs_CoverTrust(t *testing.T) {
	for _, id := range TrustSettingIDs {
		require.True(t, WorkspaceDenied(id), id)
	}
	for _, id := range WorkspaceDenyIDs {
		_, ok := Find(id)
		require.True(t, ok, "%s is a setting", id)
	}
	require.False(t, WorkspaceDenied("theme"))
	require.False(t, WorkspaceDenied("default_spawn_model"))
}

// The settings beyond the trust pair that a cloned repo must not control,
// each with why. A new security-sensitive setting must be added both to
// WorkspaceDenyIDs and to this table.
func TestWorkspaceDenyIDs_Exact(t *testing.T) {
	want := map[string]string{
		"skip_permissions":   "starts agents with permission prompts off",
		"auto_trust_dirs":    "answers the agent's folder-trust dialog",
		"default_spawn_tool": "picks which agent runs (some edit and run commands without asking)",
		"extra_agents":       "turns on agents whose defaults act without asking",
		"audit_trail":        "turning it off hides finished work from the audit trail",
		"audit_full_prompt":  "copies whole prompts (secrets) into .rex/audit.jsonl, which a repo may sync or commit",
		"workspaces":         "the switch that gates .rex/ itself",
		"lua_config":         "the switch that gates every config.lua",
	}
	require.Len(t, WorkspaceDenyIDs, len(want))
	for id := range want {
		require.True(t, WorkspaceDenied(id), id)
	}
}
