package registry

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// FuzzLoad feeds arbitrary bytes to Load as a user tools.yaml. The daemon
// loads this file at boot and on SIGHUP, so a malformed file must surface as
// an error, never a panic. When Load succeeds, the merged registry must still
// meet the invariants validate enforces and keep every built-in tool (the
// user file extends the built-ins; it cannot remove them).
func FuzzLoad(f *testing.F) {
	if user, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "tools-user.yaml")); err == nil {
		f.Add(user)
	}
	for _, s := range []string{
		"tools: [oops",
		"",
		"tools: []\n",
		"tools:\n  - id: echo\n    models:\n      - id: short\n        args: [\"echo hi\"]\n",
		"tools:\n  - id: t1\n    models: [{id: m1}]\n    detect: {kind: heuristic, prompt_regex: \"> \", idle_ms: 100, done_regex: \"[unclosed\"}\n",
		"tools:\n  - id: \"\"\n",
		"tools:\n  - id: x\n    models: [{id: m}]\n    detect: {kind: hooks}\n",
	} {
		f.Add([]byte(s))
	}

	builtin, err := Load("")
	require.NoError(f, err)

	f.Fuzz(func(t *testing.T, data []byte) {
		path := filepath.Join(t.TempDir(), "tools.yaml")
		require.NoError(t, os.WriteFile(path, data, 0o600))

		reg, err := Load(path)
		if err != nil {
			require.Nil(t, reg)
			return
		}
		require.NotNil(t, reg)
		require.NoError(t, validate(reg.Tools), "Load returned a registry that fails validate")
		for _, b := range builtin.Tools {
			_, ok := reg.Find(b.ID)
			require.True(t, ok, "built-in tool %q dropped by the user file", b.ID)
		}
	})
}
