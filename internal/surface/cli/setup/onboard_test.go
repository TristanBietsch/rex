package setup

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/surface/tui"
)

// agentsOnPath gives the test a PATH holding only stubs for bins.
func agentsOnPath(t *testing.T, bins ...string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	for _, b := range bins {
		file, body := b, "#!/bin/sh\nexit 0\n"
		if runtime.GOOS == "windows" {
			file, body = b+".bat", "@echo off\r\n"
		}
		require.NoError(t, os.WriteFile(filepath.Join(dir, file), []byte(body), 0o755)) //nolint:gosec // a test stub must be executable
	}
}

// fakeOnboardDaemon replaces the tour's daemon seams: the install checks
// pass, and the daemon sees daemon (nil: no daemon). It counts setup runs.
func fakeOnboardDaemon(t *testing.T, daemon map[string]bool) *int {
	t.Helper()
	setups := 0
	oldTools, oldChecks, oldSetup := onboardDaemonTools, onboardInstallChecks, onboardRunSetup
	onboardDaemonTools = func(string) (map[string]bool, bool) { return daemon, daemon != nil }
	onboardInstallChecks = func(string, bool) []checkResult {
		return []checkResult{{"daemon reachable", checkPass, "fake"}}
	}
	onboardRunSetup = func(string, string, string) error { setups++; return nil }
	t.Cleanup(func() { onboardDaemonTools, onboardInstallChecks, onboardRunSetup = oldTools, oldChecks, oldSetup })
	return &setups
}

// freshHome gives the test an empty home: no config, no onboarded marker.
func freshHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
}

// tourAt installs a terminal Printer that reads input and writes to the
// returned buffer.
func tourAt(t *testing.T, input string) *bytes.Buffer {
	t.Helper()
	freshHome(t)
	atTerminal(t)
	var out bytes.Buffer
	p, err := core.Out().WithIO(&out, &out, strings.NewReader(input))
	require.NoError(t, err)
	core.SetOut(p)
	return &out
}

func exitCode(t *testing.T, err error) int {
	t.Helper()
	var ec core.ExitCoder
	require.ErrorAs(t, err, &ec)
	return ec.ExitCode()
}

// With two agents installed, two are usable; the rest carry install hints.
func TestScanAgents_TwoInstalled(t *testing.T) {
	agentsOnPath(t, "claude", "agy")
	agents := scanAgents()
	require.Equal(t, 2, usableAgents(agents))
	for _, a := range agents {
		switch a.ID {
		case "claude", "antigravity":
			require.True(t, a.Installed, a.ID)
			require.NotEmpty(t, a.Path)
		default:
			require.False(t, a.Installed, a.ID)
			require.NotEmpty(t, a.Hint, a.ID)
		}
	}
}

// An extra agent installed while extra_agents is off is not usable.
func TestScanAgents_ExtraAgentOff(t *testing.T) {
	agentsOnPath(t, "qwen")
	freshHome(t)
	cfg, err := paths.ConfigFile()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(cfg), 0o700))
	require.NoError(t, os.WriteFile(cfg, []byte("extra_agents: false\n"), 0o600))

	agents := scanAgents()
	require.Zero(t, usableAgents(agents))
	rows := agentChecks(agents, nil)
	var qwen checkResult
	for _, r := range rows {
		if strings.Contains(r.Name, "(qwen)") {
			qwen = r
		}
	}
	require.Contains(t, qwen.Detail, "rex config set extra_agents true")
	require.Equal(t, checkFail, rows[len(rows)-1].Status)
}

// An agent the CLI finds but the daemon does not is a stale daemon PATH.
func TestAgentChecks_StaleDaemonPath(t *testing.T) {
	agentsOnPath(t, "claude")
	rows := agentChecks(scanAgents(), map[string]bool{"claude": false})
	require.Equal(t, checkWarn, rows[0].Status)
	require.Contains(t, rows[0].Detail, "rex daemon restart -cold")
	require.Equal(t, checkPass, rows[len(rows)-1].Status, "the CLI still counts it")
}

// -check with no agent: exit 1, every agent in the payload.
func TestRunOnboard_CheckJSON(t *testing.T) {
	agentsOnPath(t)
	freshHome(t)
	fakeOnboardDaemon(t, nil)
	installPrinter(false)
	var out bytes.Buffer
	p, err := core.Out().WithIO(&out, &out, strings.NewReader(""))
	require.NoError(t, err)
	core.SetOut(p)
	t.Cleanup(func() { core.SetOut(nil) })

	err = RunOnboard([]string{"-check", "-o", "json"})
	require.Equal(t, 1, exitCode(t, err))
	var env struct {
		Data onboardData `json:"data"`
	}
	require.NoError(t, json.Unmarshal(out.Bytes(), &env), out.String())
	require.False(t, env.Data.OK)
	require.Len(t, env.Data.Agents, len(tui.KnownAgents(loadAgentRegistry())))
	for _, a := range env.Data.Agents {
		require.False(t, a.Installed)
		require.Nil(t, a.DaemonInstalled)
	}
}

// The tour refuses without a terminal and points at -check.
func TestRunOnboard_NeedsTerminal(t *testing.T) {
	installPrinter(false)
	t.Cleanup(func() { core.SetOut(nil) })
	err := RunOnboard(nil)
	require.Equal(t, int(core.ExitInvalidArgs), exitCode(t, err))
	require.Contains(t, err.Error(), "-check")
}

func TestRunOnboard_BadFlags(t *testing.T) {
	installPrinter(false)
	t.Cleanup(func() { core.SetOut(nil) })
	require.Equal(t, int(core.ExitInvalidArgs), exitCode(t, RunOnboard([]string{"-check", "-step", "2"})))
	require.Equal(t, int(core.ExitInvalidArgs), exitCode(t, RunOnboard([]string{"-step", "9"})))
}

// enter walks all five steps, teaches the board keys from the key table
// and writes the onboarded marker.
func TestTour_WalksEveryStep(t *testing.T) {
	agentsOnPath(t, "claude")
	fakeOnboardDaemon(t, map[string]bool{"claude": true})
	out := tourAt(t, strings.Repeat("\n", onboardSteps))
	require.True(t, NeedsOnboarding())

	require.NoError(t, RunOnboard(nil))
	got := out.String()
	for i, s := range tourSteps {
		require.Contains(t, got, "["+string(rune('1'+i))+"/5] "+s.title)
	}
	cc, ok := tui.BoardKeyDesc("cc")
	require.True(t, ok)
	require.Contains(t, got, cc)
	require.Contains(t, got, "Claude Code (claude)")
	require.False(t, NeedsOnboarding(), "the marker is written")
}

// With no agent, step 2 blocks: enter does not move on, q exits 1 with
// the resume command.
func TestTour_NoAgentBlocks(t *testing.T) {
	agentsOnPath(t)
	fakeOnboardDaemon(t, map[string]bool{})
	out := tourAt(t, "\n\nq\n")

	err := RunOnboard(nil)
	require.Equal(t, 1, exitCode(t, err))
	got := out.String()
	require.Contains(t, got, "rex needs at least one coding agent")
	require.Contains(t, got, "fix the failed check first")
	require.Contains(t, got, "resume with: rex onboard -step 2")
	require.NotContains(t, got, "[3/5]")
}

// r on step 2 rescans: an agent installed meanwhile unblocks the tour.
func TestTour_RescanFindsNewAgent(t *testing.T) {
	agentsOnPath(t)
	freshHome(t)
	dir := os.Getenv("PATH")
	fakeOnboardDaemon(t, nil)
	in, w, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { _ = in.Close() })
	atTerminal(t)
	var out bytes.Buffer
	p, err := core.Out().WithIO(&out, &out, in)
	require.NoError(t, err)
	core.SetOut(p)

	done := make(chan error, 1)
	go func() { done <- RunOnboard([]string{"-step", "2"}) }()
	_, _ = w.WriteString("\n") // blocked: no agent
	file, body := "claude", "#!/bin/sh\nexit 0\n"
	if runtime.GOOS == "windows" {
		file, body = "claude.bat", "@echo off\r\n"
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, file), []byte(body), 0o755)) //nolint:gosec // a test stub must be executable
	_, _ = w.WriteString("r\n\nq\n")
	require.NoError(t, <-done)
	require.Contains(t, out.String(), "[3/5] Configure")
}

// s on step 3 runs rex setup and shows the step again.
func TestTour_SetupKey(t *testing.T) {
	agentsOnPath(t, "claude")
	setups := fakeOnboardDaemon(t, map[string]bool{"claude": true})
	out := tourAt(t, "s\nq\n")
	require.NoError(t, RunOnboard([]string{"-step", "3"}))
	require.Equal(t, 1, *setups)
	require.Equal(t, 2, strings.Count(out.String(), "[3/5] Configure"))
}

// A default agent that is not installed gets a warning naming the ones
// that are.
func TestTour_DefaultToolNotInstalled(t *testing.T) {
	agentsOnPath(t, "claude")
	fakeOnboardDaemon(t, map[string]bool{"claude": true})
	out := tourAt(t, "q\n")
	cfg, err := paths.ConfigFile()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(cfg), 0o700))
	require.NoError(t, os.WriteFile(cfg, []byte("default_spawn_tool: codex\n"), 0o600))
	require.NoError(t, RunOnboard([]string{"-step", "3"}))
	require.Contains(t, out.String(), `the default agent "codex" is not installed`)
	require.Contains(t, out.String(), "rex config set default_spawn_tool claude")
}

// Every key the tour teaches is a real board binding.
func TestTourKeysAreBound(t *testing.T) {
	for _, k := range tourKeys {
		_, ok := tui.BoardKeyDesc(k)
		require.True(t, ok, "tour key %q is not in the board's key table", k)
	}
}

// Step 1 starts a daemon that is not running, and reports a start that
// fails; without start it only warns.
func TestInstallChecks_StartsDaemon(t *testing.T) {
	old := onboardStartDaemon
	t.Cleanup(func() { onboardStartDaemon = old })
	socket := filepath.Join(t.TempDir(), "none.sock")
	find := func(rows []checkResult) checkResult {
		for _, r := range rows {
			if r.Name == "daemon reachable" {
				return r
			}
		}
		t.Fatal("no daemon row")
		return checkResult{}
	}

	started := 0
	onboardStartDaemon = func(string) error { started++; return nil }
	require.Equal(t, checkPass, find(installChecks(socket, true)).Status)
	require.Equal(t, 1, started)

	onboardStartDaemon = func(string) error { return os.ErrPermission }
	require.Equal(t, checkFail, find(installChecks(socket, true)).Status)

	require.Equal(t, checkWarn, find(installChecks(socket, false)).Status)
	require.Equal(t, 1, started, "-check never starts the daemon")
}

// No daemon on the socket: no daemon view.
func TestDaemonTools_NoDaemon(t *testing.T) {
	got, ok := daemonTools(filepath.Join(t.TempDir(), "none.sock"))
	require.False(t, ok)
	require.Nil(t, got)
}

// -check in text lists every row, info included, under its own header.
func TestRunOnboard_CheckText(t *testing.T) {
	agentsOnPath(t, "claude")
	freshHome(t)
	fakeOnboardDaemon(t, map[string]bool{"claude": true})
	atTerminal(t)
	var out bytes.Buffer
	p, err := core.Out().WithIO(&out, &out, strings.NewReader(""))
	require.NoError(t, err)
	core.SetOut(p)

	require.NoError(t, RunOnboard([]string{"-check"}))
	require.Contains(t, out.String(), "Onboard check")
	require.Contains(t, out.String(), "Claude Code (claude)")
	require.Contains(t, out.String(), "OpenAI Codex (codex)", "info rows are shown")
}

// b goes back a step; an unknown key is named and the step stays.
func TestTour_BackAndUnknownKey(t *testing.T) {
	agentsOnPath(t, "claude")
	fakeOnboardDaemon(t, map[string]bool{"claude": true})
	out := tourAt(t, "b\nzz\nq\n")
	require.NoError(t, RunOnboard([]string{"-step", "4"}))
	got := out.String()
	require.Contains(t, got, "[3/5] Configure")
	require.Contains(t, got, `unknown key "zz"`)
}

// End of input quits like q.
func TestTour_EOFQuits(t *testing.T) {
	agentsOnPath(t, "claude")
	fakeOnboardDaemon(t, map[string]bool{"claude": true})
	out := tourAt(t, "")
	require.NoError(t, RunOnboard(nil))
	require.Contains(t, out.String(), "resume with: rex onboard -step 1")
}
