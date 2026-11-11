package server

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/daemon/adapter"
)

// legacyArgv is handleNewSession's inline argv construction as it was before
// buildArgv was extracted. The golden test pins buildArgv to it.
func legacyArgv(tool registry.Tool, model registry.Model, effort string, trust, skip []string) []string {
	cmdArgs := append([]string{}, tool.Command...)
	cmdArgs = append(cmdArgs, model.Args...)
	if model.Effort != nil && effort != "" && model.Effort.ArgTemplate != "" {
		rendered := strings.ReplaceAll(model.Effort.ArgTemplate, "{value}", effort)
		cmdArgs = append(cmdArgs, strings.Fields(rendered)...)
	}
	cmdArgs = append(cmdArgs, trust...)
	cmdArgs = append(cmdArgs, skip...)
	if tool.Detect.Kind == "hooks" && tool.Detect.Format == "claude" {
		cmdArgs = append(cmdArgs, adapter.ClaudeHookArgs()...)
	}
	return cmdArgs
}

func TestBuildArgvGoldenEveryBuiltin(t *testing.T) {
	reg, err := registry.Load("")
	require.NoError(t, err)
	n := 0
	for _, tool := range reg.Tools {
		trust := []string{"--trusted", "x y"}
		for _, model := range tool.Models {
			efforts := []string{""}
			if model.Effort != nil {
				efforts = append(efforts, model.Effort.Options...)
			}
			for _, effort := range efforts {
				for _, tr := range [][]string{nil, trust} {
					for _, skip := range []bool{false, true} {
						var skipArgs []string
						if skip {
							skipArgs = tool.SkipPermissionsArgs
						}
						spec := spawnSpec{Tool: tool, Model: model, Effort: effort, CWD: "/w",
							Trust: trustDecision{Args: tr}, SkipPermissions: skip, HookFile: "/h", SessionID: "sid"}
						require.Equal(t, legacyArgv(tool, model, effort, tr, skipArgs), buildArgv(spec),
							"%s/%s effort=%q trust=%v skip=%v", tool.ID, model.ID, effort, tr, skip)
						n++
					}
				}
			}
		}
	}
	require.Greater(t, n, 50)
}

func TestBuildArgvLiterals(t *testing.T) {
	reg, err := registry.Load("")
	require.NoError(t, err)
	tool, model, ok := reg.FindModel("codex", "gpt-5.5")
	require.True(t, ok)
	require.Equal(t,
		[]string{"codex", "--model", "gpt-5.5", "-c", "model_reasoning_effort=high"},
		buildArgv(spawnSpec{Tool: tool, Model: model, Effort: "high"}))

	tool, model, ok = reg.FindModel("claude", "opus")
	require.True(t, ok)
	got := buildArgv(spawnSpec{Tool: tool, Model: model, Effort: "max", Trust: trustDecision{Args: []string{"--t"}}})
	want := append([]string{}, tool.Command...)
	want = append(want, "--model", "opus", "--effort=max", "--t")
	require.Equal(t, want, got[:len(want)])
	require.Equal(t, adapter.ClaudeHookArgs(), got[len(want):], "hook args come last, before the prompt")
}

func TestChildEnvFor(t *testing.T) {
	hook := registry.Tool{Detect: registry.Detect{Kind: "hooks"}}
	hf := filepath.Join(t.TempDir(), "hooks.log")
	require.Equal(t,
		[]string{adapter.HookFileEnv + "=" + filepath.ToSlash(hf), "REX_SESSION_ID=abc"},
		childEnvFor(spawnSpec{Tool: hook, HookFile: hf, SessionID: "abc"}))

	require.Equal(t, []string{"REX_SESSION_ID=abc"},
		childEnvFor(spawnSpec{Tool: registry.Tool{}, HookFile: hf, SessionID: "abc"}),
		"no hook env for tools that are not hook-detected")
}

func TestBuildArgvSkipPermissions(t *testing.T) {
	reg, err := registry.Load("")
	require.NoError(t, err)
	for _, id := range []string{"claude", "antigravity", "echo"} {
		tool, ok := reg.Find(id)
		require.True(t, ok)
		model := tool.Models[0]
		off := buildArgv(spawnSpec{Tool: tool, Model: model})
		require.NotContains(t, off, "--dangerously-skip-permissions", "%s: setting off", id)
		on := buildArgv(spawnSpec{Tool: tool, Model: model, SkipPermissions: true})
		if id == "echo" {
			require.Equal(t, off, on, "echo has no skip_permissions_args")
		} else {
			require.Contains(t, on, "--dangerously-skip-permissions", "%s: setting on", id)
		}
	}
}
