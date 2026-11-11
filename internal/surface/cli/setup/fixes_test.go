package setup

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The version probe runs the new binary with plain output, even when the
// caller is in an agent session (CLAUDECODE would make it print an agent line).
func TestVersionOfRunsChildWithPlainOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script stand-in")
	}
	t.Setenv("CLAUDECODE", "1")
	t.Setenv("REX_OUTPUT", "json")
	t.Setenv("REX_AGENT", "1")
	exe := filepath.Join(t.TempDir(), "rex")
	script := "#!/bin/sh\nif [ \"$REX_AGENT\" = 0 ] && [ \"$REX_OUTPUT\" = txt ]; then echo v9.9.9; else echo \"ok cmd=version version=v9.9.9\"; fi\n"
	require.NoError(t, os.WriteFile(exe, []byte(script), 0o700)) //nolint:gosec // G306: an executable stand-in.
	require.Equal(t, "v9.9.9", versionOf(exe))
	require.Equal(t, "v1", versionOf(filepath.Join(t.TempDir(), "missing")), "falls back to the running version")
}

// With shell integration on, the preview shows the shell block with its
// line breaks in every human format.
func TestSetupPreviewKeepsShellBlockLines(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/zsh")
	for _, f := range []string{"text", "txt", "md"} {
		p, out, _ := viewPrinter(t, f, 80, false)
		require.NoError(t, emitSetupPreview(p, filepath.Join(home, "config.yaml"), "/s/rex.sock", "", "", "", "", false), f)
		lines := strings.Split(out.String(), "\n")
		block := 0
		for _, l := range lines {
			if strings.Contains(l, "BEGIN REX") || strings.Contains(l, "END REX") || strings.Contains(l, "rex completion") {
				block++
			}
		}
		require.GreaterOrEqual(t, block, 2, "%s: the block is several lines:\n%s", f, out.String())
	}
}
