package registry

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateSkillArgs(t *testing.T) {
	base := Tool{ID: "x", Models: []Model{{ID: "m"}}, Detect: Detect{Kind: "hooks", Format: "claude"}}
	ok := base
	ok.SkillArgs = []string{"--plugin-dir", "{plugin_dir}"}
	require.NoError(t, validate([]Tool{ok}))

	noDir := base
	noDir.SkillArgs = []string{"--plugin-dir", "somewhere"}
	require.ErrorContains(t, validate([]Tool{noDir}), "must name the plugin folder with {plugin_dir}")

	empty := base
	empty.SkillArgs = []string{"--plugin-dir", "{plugin_dir}", ""}
	require.ErrorContains(t, validate([]Tool{empty}), "skill_args")
}

func TestMergeSkillArgs(t *testing.T) {
	merged := mergeOne(Tool{ID: "a", SkillArgs: []string{"--old", "{plugin_dir}"}}, Tool{ID: "a", SkillArgs: []string{"--new", "{plugin_dir}"}})
	require.Equal(t, []string{"--new", "{plugin_dir}"}, merged.SkillArgs)
	kept := mergeOne(Tool{ID: "a", SkillArgs: []string{"--old", "{plugin_dir}"}}, Tool{ID: "a", Name: "x"})
	require.Equal(t, []string{"--old", "{plugin_dir}"}, kept.SkillArgs)
}

func TestBuiltinClaudeSkillArgs(t *testing.T) {
	reg, err := Load("")
	require.NoError(t, err)
	tool, ok := reg.Find("claude")
	require.True(t, ok)
	require.Equal(t, []string{"--plugin-dir", "{plugin_dir}"}, tool.SkillArgs)
}

func TestBuiltinAntigravityHasNoSkillArgs(t *testing.T) {
	reg, err := Load("")
	require.NoError(t, err)
	tool, ok := reg.Find("antigravity")
	require.True(t, ok)
	require.Empty(t, tool.SkillArgs)
}
