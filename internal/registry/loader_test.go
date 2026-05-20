package registry

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoad_BuiltinOnly(t *testing.T) {
	reg, err := Load("")
	require.NoError(t, err)

	echo, ok := reg.Find("echo")
	require.True(t, ok)
	require.Equal(t, "Echo (test)", echo.Name)
	require.Len(t, echo.Models, 3)
}

func TestLoad_UserExtends(t *testing.T) {
	wd, _ := os.Getwd()
	user := filepath.Join(wd, "..", "..", "testdata", "tools-user.yaml")

	reg, err := Load(user)
	require.NoError(t, err)

	echo, _ := reg.Find("echo")
	require.Len(t, echo.Models, 4) // 3 builtin + 1 user-extra

	user2, ok := reg.Find("usertool")
	require.True(t, ok)
	require.Equal(t, "#FF00FF", user2.Color)
}

func TestLoad_BadYAMLFails(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "bad.yaml")
	require.NoError(t, os.WriteFile(tmp, []byte("tools: [oops"), 0o644))
	_, err := Load(tmp)
	require.Error(t, err)
}

func TestLoad_RealToolsPresent(t *testing.T) {
	reg, err := Load("")
	require.NoError(t, err)
	for _, id := range []string{"claude", "codex", "gemini", "ollama", "grok", "deepseek", "kimi"} {
		_, ok := reg.Find(id)
		require.True(t, ok, "tool %s missing", id)
	}
}

func TestLoad_OptInDefaults(t *testing.T) {
	reg, err := Load("")
	require.NoError(t, err)
	for _, id := range []string{"grok", "deepseek", "kimi"} {
		tool, ok := reg.Find(id)
		require.True(t, ok)
		require.NotNil(t, tool.EnabledByDefault, "tool %s should have explicit enabled_by_default", id)
		require.False(t, *tool.EnabledByDefault, "tool %s should be opt-in", id)
	}
}

func TestValidate_HeuristicWithoutDoneRegex(t *testing.T) {
	tools := []Tool{{
		ID: "t1", Models: []Model{{ID: "m1"}},
		Detect: Detect{Kind: "heuristic", PromptRegex: "> ", IdleMs: 100},
	}}
	require.NoError(t, validate(tools))
}

func TestValidate_HeuristicWithValidDoneRegex(t *testing.T) {
	tools := []Tool{{
		ID: "t1", Models: []Model{{ID: "m1"}},
		Detect: Detect{Kind: "heuristic", PromptRegex: "> ", DoneRegex: "^✓ done$", IdleMs: 100},
	}}
	require.NoError(t, validate(tools))
}

func TestValidate_HeuristicWithInvalidDoneRegex(t *testing.T) {
	tools := []Tool{{
		ID: "t1", Models: []Model{{ID: "m1"}},
		Detect: Detect{Kind: "heuristic", PromptRegex: "> ", DoneRegex: "[unclosed", IdleMs: 100},
	}}
	err := validate(tools)
	require.Error(t, err)
	require.Contains(t, err.Error(), "done_regex")
}
