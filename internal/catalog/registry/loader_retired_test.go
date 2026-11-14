package registry

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// builtinWithout returns the built-in tools minus the given ids, standing in
// for a future builtin.yaml that no longer ships them.
func builtinWithout(t *testing.T, ids ...string) []Tool {
	t.Helper()
	var f File
	require.NoError(t, yaml.Unmarshal(BuiltinBytes(), &f))
	var out []Tool
	for _, tool := range f.Tools {
		if !slices.Contains(ids, tool.ID) {
			out = append(out, tool)
		}
	}
	return out
}

func writeTools(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tools.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

// Regression for commit ab92413: a partial overlay of a tool that is no
// longer built in (here gemini: just a model, no command or detect) used to
// fail validation and stop the daemon from starting.
func TestLoad_RetiredPartialOverlayDropped(t *testing.T) {
	path := writeTools(t, `tools:
  - id: gemini
    models:
      - id: 2.5-pro
        name: Pro
  - id: codex
    name: Codex
`)
	reg, err := loadWith(builtinWithout(t, "gemini", "codex"), path)
	require.NoError(t, err)
	_, ok := reg.Find("gemini")
	require.False(t, ok)
	_, ok = reg.Find("codex")
	require.False(t, ok)
	require.Len(t, reg.Warnings, 2)
	require.Contains(t, reg.Warnings[0], "tool gemini is no longer built in")
	require.Contains(t, reg.Warnings[0], "docs/registry.md")
	require.Contains(t, reg.Warnings[1], "tool codex")
}

func TestLoad_RetiredFullOverlayLoads(t *testing.T) {
	path := writeTools(t, `tools:
  - id: gemini
    name: Gemini
    category: paid
    command: ["gemini"]
    detect:
      kind: heuristic
      prompt_regex: "> "
      idle_ms: 500
    models:
      - id: pro
        name: Pro
`)
	reg, err := loadWith(builtinWithout(t, "gemini"), path)
	require.NoError(t, err)
	require.Empty(t, reg.Warnings)
	_, ok := reg.Find("gemini")
	require.True(t, ok)
}

func TestLoad_UnknownPartialOverlayStillFails(t *testing.T) {
	path := writeTools(t, `tools:
  - id: gemni
    models:
      - id: pro
        name: Pro
`)
	_, err := loadWith(builtinWithout(t, "gemini"), path)
	require.ErrorContains(t, err, "gemni")
}

func TestLoad_BuiltinPartialOverlayStillMerges(t *testing.T) {
	path := writeTools(t, `tools:
  - id: gemini
    models:
      - id: extra
        name: Extra
`)
	reg, err := Load(path)
	require.NoError(t, err)
	require.Empty(t, reg.Warnings)
}

func TestValidateTool(t *testing.T) {
	require.ErrorContains(t, validateTool(Tool{}), "empty id")
	require.ErrorContains(t, validateTool(Tool{ID: "x"}), "no models")
}
