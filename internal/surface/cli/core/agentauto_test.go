package core

import (
	"bytes"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// CLAUDECODE=1 with a piped stdout (a go test run is one) switches agent
// mode on; REX_AGENT=0, the cli_agent_auto feature and an explicit format
// each override it.
func TestInitOutputAutoAgent(t *testing.T) {
	t.Cleanup(func() { SetOut(nil) })
	home := t.TempDir()
	t.Setenv("HOME", home)

	_, err := InitOutput([]string{"ls"}, []string{"CLAUDECODE=1"})
	require.NoError(t, err)
	require.Equal(t, FormatAgent, Out().Format)
	require.True(t, Out().AgentAuto)

	_, err = InitOutput([]string{"ls"}, []string{"CLAUDECODE=1", "REX_AGENT=0"})
	require.NoError(t, err)
	require.NotEqual(t, FormatAgent, Out().Format)
	require.False(t, Out().AgentAuto)

	_, err = InitOutput([]string{"-o", "json", "ls"}, []string{"CLAUDECODE=1"})
	require.NoError(t, err)
	require.Equal(t, FormatJSON, Out().Format)

	_, err = InitOutput([]string{"ls"}, []string{"CLAUDECODE=1", "REX_OUTPUT=md"})
	require.NoError(t, err)
	require.Equal(t, FormatMD, Out().Format)

	_, err = InitOutput([]string{"ls"}, []string{"REX_AGENT=1"})
	require.NoError(t, err)
	require.Equal(t, FormatAgent, Out().Format)
	require.False(t, Out().AgentAuto, "asked for, not detected")

	// Switched off in Settings -> Features: only the explicit ways remain.
	cfg := filepath.Join(home, ".config", "rex", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(cfg), 0o700))
	require.NoError(t, os.WriteFile(cfg, []byte("cli_agent_auto: false\n"), 0o600))
	_, err = InitOutput([]string{"ls"}, []string{"CLAUDECODE=1"})
	require.NoError(t, err)
	require.NotEqual(t, FormatAgent, Out().Format)
	_, err = InitOutput([]string{"--agent", "ls"}, []string{"CLAUDECODE=1"})
	require.NoError(t, err)
	require.Equal(t, FormatAgent, Out().Format)
}

// Without CLAUDECODE the settings file is never read to decide the format.
func TestInitOutputNoClaudeCodeIgnoresTheFeature(t *testing.T) {
	t.Cleanup(func() { SetOut(nil) })
	t.Setenv("HOME", t.TempDir())
	_, err := InitOutput([]string{"ls"}, []string{"CLAUDECODE="})
	require.NoError(t, err)
	require.False(t, Out().AgentAuto)
	require.Equal(t, FormatTXT, Out().Format, "a pipe gets txt")
}

func TestRequireText(t *testing.T) {
	for f, ok := range map[string]bool{"text": true, "txt": false, "md": false, "json": false, "agent": false} {
		p, err := NewPrinter(FormatFlags{Output: f}, Env{Getenv: envOf(), Width: 80, Scheme: "default", GOOS: "linux"}, io.Discard, io.Discard)
		require.NoError(t, err)
		err = p.RequireText("rex", "rex ls")
		if ok {
			require.NoError(t, err, f)
			continue
		}
		var ee ExitError
		require.True(t, errors.As(err, &ee), f)
		require.Equal(t, ExitInvalidArgs, ee.Code)
		require.Equal(t, "rex ls", ee.Hint)
		require.Contains(t, ee.Msg, "interactive")
		require.Contains(t, ee.Msg, f+" mode")
	}
}

func TestOutputFlagsHintOnRefusedFormat(t *testing.T) {
	var out bytes.Buffer
	p, err := NewPrinter(FormatFlags{}, Env{Getenv: envOf(), Width: 80, Scheme: "default", GOOS: "linux"}, &out, io.Discard)
	require.NoError(t, err)
	SetOut(p)
	t.Cleanup(func() { SetOut(nil) })
	fs := flag.NewFlagSet("attach", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	o := AddOutputFlags(fs, TextFormats, WithHint("rex log <sel> -n 50"))
	_, err = o.Parse(fs, []string{"-o", "agent"})
	var ee ExitError
	require.True(t, errors.As(err, &ee))
	require.Equal(t, ExitInvalidArgs, ee.Code)
	require.Equal(t, "rex log <sel> -n 50", ee.Hint)
	require.Contains(t, ee.Msg, `output "agent" is not supported (supported: text, txt)`)
}
