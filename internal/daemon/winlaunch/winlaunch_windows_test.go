//go:build windows

package winlaunch

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

// argsOutEnv makes the test binary a stand-in agent: it writes its
// arguments as JSON to the named file and exits.
const argsOutEnv = "WINLAUNCH_TEST_ARGS_OUT"

func TestMain(m *testing.M) {
	if out := os.Getenv(argsOutEnv); out != "" {
		b, _ := json.Marshal(os.Args[1:])
		if err := os.WriteFile(out, b, 0o600); err != nil {
			os.Exit(2)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// stageAgent copies the test binary to dir\node_modules\pkg\bin\agent.exe.
func stageAgent(t *testing.T, dir string) string {
	t.Helper()
	self, err := os.Executable()
	require.NoError(t, err)
	exe := filepath.Join(dir, "node_modules", "pkg", "bin", "agent.exe")
	require.NoError(t, os.MkdirAll(filepath.Dir(exe), 0o700))
	src, err := os.Open(self)
	require.NoError(t, err)
	defer src.Close()
	dst, err := os.OpenFile(exe, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o700)
	require.NoError(t, err)
	_, err = io.Copy(dst, src)
	require.NoError(t, err)
	require.NoError(t, dst.Close())
	return exe
}

// runArgs starts argv in dir and returns the arguments the child saw.
func runArgs(t *testing.T, dir string, argv []string) []string {
	t.Helper()
	out := filepath.Join(dir, "args.json")
	_ = os.Remove(out)
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), argsOutEnv+"="+out)
	b, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", b)
	raw, err := os.ReadFile(out)
	require.NoError(t, err)
	var got []string
	require.NoError(t, json.Unmarshal(raw, &got))
	return got
}

// The real thing: an npm shim is resolved to its exe, which receives every
// injection payload byte for byte, and cmd.exe runs none of it.
func TestPlanResolvedShimEndToEnd(t *testing.T) {
	dir := t.TempDir()
	exe := stageAgent(t, dir)
	shim := filepath.Join(dir, "agent.cmd")
	require.NoError(t, os.WriteFile(shim, []byte(`@ECHO off
GOTO start
:find_dp0
SET dp0=%~dp0
EXIT /b
:start
SETLOCAL
CALL :find_dp0
"%dp0%\node_modules\pkg\bin\agent.exe"   %*
`), 0o600))
	for _, p := range append(injectionPayloads, "& echo pwned > marker.txt", `" & echo pwned > marker.txt & "`) {
		l, err := Plan(Request{Base: []string{shim, "--model", "opus"}, PromptArgs: []string{"--", p}, Prompt: p}, SystemEnv())
		require.NoError(t, err)
		require.Equal(t, "shim:exe", l.Via)
		require.True(t, strings.EqualFold(exe, l.Argv[0]), l.Argv[0])
		got := runArgs(t, dir, l.Argv)
		require.Equal(t, []string{"--model", "opus", "--", p}, got, "%q", p)
		_, err = os.Stat(filepath.Join(dir, "marker.txt"))
		require.True(t, os.IsNotExist(err), "%q ran as cmd.exe syntax", p)
	}
}

// A shim rex cannot resolve runs through cmd.exe with safe arguments only;
// they reach the program unchanged and the prompt is left for the paste.
func TestPlanUnresolvedShimEndToEnd(t *testing.T) {
	dir := t.TempDir()
	stageAgent(t, dir)
	shim := filepath.Join(dir, "hand.cmd")
	require.NoError(t, os.WriteFile(shim, []byte("@\"%~dp0node_modules\\pkg\\bin\\agent.exe\" %1 %2 %3\r\n"), 0o600))
	p := "x & echo pwned > marker.txt"
	l, err := Plan(Request{Base: []string{shim, "--model", "opus"}, PromptArgs: []string{"--", p}, Prompt: p}, SystemEnv())
	require.NoError(t, err)
	require.Equal(t, "shim:safe-args", l.Via)
	require.Equal(t, p, l.Paste)
	require.Equal(t, []string{"--model", "opus"}, runArgs(t, dir, l.Argv))
	_, err = os.Stat(filepath.Join(dir, "marker.txt"))
	require.True(t, os.IsNotExist(err))
}

// CmdLineBound never under-counts what Go hands CreateProcess.
func TestCmdLineBoundCoversComposeCommandLine(t *testing.T) {
	args := append([]string{`C:\Program Files\x\agent.exe`, "", `a\`, `a b\`, `\\server\share\`}, injectionPayloads...)
	for i := range args {
		got := len(utf16.Encode([]rune(windows.ComposeCommandLine(args[:i+1]))))
		require.GreaterOrEqual(t, CmdLineBound(args[:i+1]), got, "%q", args[:i+1])
	}
}
