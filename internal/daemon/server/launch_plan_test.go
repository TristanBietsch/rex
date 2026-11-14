package server

import (
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/daemon/winlaunch"
)

// fakeWinEnv resolves every command to a .cmd shim nothing can resolve, so
// the prompt must leave argv.
func fakeWinEnv(resolved map[string]string) *winlaunch.Env {
	return &winlaunch.Env{
		LookPath: func(name string) (string, error) {
			if p, ok := resolved[name]; ok {
				return p, nil
			}
			return "", exec.ErrNotFound
		},
		ReadFile: func(string) ([]byte, error) { return nil, errors.New("unreadable") },
		IsFile:   func(string) bool { return false },
	}
}

func TestPlanLaunchOffOutsideWindows(t *testing.T) {
	if !winlaunch.Active {
		require.Nil(t, launchEnv)
	}
	argv, paste, d, err := planLaunch(nil, "s", []string{"claude", "--", "x & calc"}, "", promptDelivery{promptArgs: 2, prompt: "x & calc"})
	require.NoError(t, err)
	require.Equal(t, []string{"claude", "--", "x & calc"}, argv)
	require.Empty(t, paste)
	require.Equal(t, 2, d.promptArgs)
}

// Every builtin prompt_args template: through an unresolvable .cmd shim the
// prompt is pasted, never put on cmd.exe's command line.
func TestPlanLaunchMovesPromptOffShim(t *testing.T) {
	reg, err := registry.Load("")
	require.NoError(t, err)
	payload := `fix "it" & calc | whoami %PATH% ^ !x!` + "\r\nnext"
	for _, tool := range reg.Tools {
		if len(tool.PromptArgs) == 0 {
			continue
		}
		base := append([]string{}, tool.Command...)
		d := initialPromptDelivery(tool, base, payload)
		env := fakeWinEnv(map[string]string{tool.Command[0]: `C:\npm\` + tool.Command[0] + `.cmd`})
		argv, paste, nd, err := planLaunch(env, "s", d.argv, "", d)
		require.NoError(t, err, tool.ID)
		require.Equal(t, strings.TrimSpace(payload), paste, tool.ID)
		require.Equal(t, `C:\npm\`+tool.Command[0]+`.cmd`, argv[0], tool.ID)
		require.Equal(t, base[1:], argv[1:], "%s: no prompt args left", tool.ID)
		require.Equal(t, promptDelivery{}, nd, "%s: bracketed paste", tool.ID)
	}
}

func TestPlanLaunchRefusesAndKeepsPastePrompt(t *testing.T) {
	env := fakeWinEnv(map[string]string{"claude": `C:\npm\claude.cmd`, "ollama": `C:\bin\ollama.exe`})
	_, _, _, err := planLaunch(env, "s", []string{"claude", "--settings", `{"a":1}`}, "", promptDelivery{})
	require.ErrorIs(t, err, winlaunch.ErrRefused)
	require.Contains(t, err.Error(), `C:\npm\claude.cmd`)

	d := promptDelivery{plainPaste: true}
	argv, paste, nd, err := planLaunch(env, "s", []string{"ollama", "run", "m"}, "hi & calc", d)
	require.NoError(t, err)
	require.Equal(t, []string{`C:\bin\ollama.exe`, "run", "m"}, argv)
	require.Equal(t, "hi & calc", paste)
	require.True(t, nd.plainPaste, "a prompt_paste tool keeps its paste mode")
}
