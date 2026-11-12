package setup

import (
	"bytes"
	"io"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/surface/tui"
	"github.com/tristanbietsch/rex/internal/testutil/golden"
)

func viewPrinter(t *testing.T, format string, width int, color bool) (*core.Printer, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	var out, errw bytes.Buffer
	env := core.Env{Getenv: func(string) string { return "" }, OutTTY: color, ErrTTY: color,
		Width: width, Scheme: "default", TrueColor: true, GOOS: "linux"}
	p, err := core.NewPrinter(core.FormatFlags{Output: format}, env, &out, &errw)
	require.NoError(t, err)
	p.Now = func() time.Time { return time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC) }
	return p, &out, &errw
}

func goldenSections(t *testing.T, build func(p *core.Printer) core.Result) []golden.Section {
	t.Helper()
	emit := func(format string, width int, color bool) string {
		p, out, _ := viewPrinter(t, format, width, color)
		require.NoError(t, p.Emit(build(p)))
		return out.String()
	}
	var secs []golden.Section
	for _, w := range []int{40, 80, 120} {
		secs = append(secs, golden.Section{Name: "text.w" + strconv.Itoa(w), Body: emit("text", w, false)})
	}
	secs = append(secs, golden.Section{Name: "text.color.w80", Body: emit("text", 80, true)})
	for _, f := range []string{"txt", "md", "json", "agent"} {
		secs = append(secs, golden.Section{Name: f, Body: emit(f, 80, false)})
	}
	return secs
}

func sampleChecks(failing bool) []checkResult {
	checks := []checkResult{
		{"rex binary", checkPass, "/home/u/.local/bin/rex"},
		{"daemon reachable", checkWarn, "socket not responding — run: rex daemon start  (socket=/home/u/.cache/rex/rex.sock)"},
		{"config dir", checkPass, "/home/u/.config/rex"},
		{"tool: claude", checkInfo, "/usr/bin/claude"},
	}
	if failing {
		checks = append(checks, checkResult{"rex-daemon binary", checkFail, "not found on PATH"})
	}
	return checks
}

func TestDoctorGolden(t *testing.T) {
	saved := doctorPlatform
	doctorPlatform = "linux/amd64"
	t.Cleanup(func() { doctorPlatform = saved })
	for _, tc := range []struct {
		name    string
		failing bool
		verbose bool
	}{{"warn", false, false}, {"fail", true, false}, {"verbose", false, true}} {
		checks := sampleChecks(tc.failing)
		fails, warns := tally(checks)
		secs := goldenSections(t, func(p *core.Printer) core.Result {
			return core.Result{Cmd: "doctor", Data: doctorResultData(checks, fails, warns),
				View: doctorView(p.Format, checks, tc.verbose, fails, warns), Failed: fails > 0}
		})
		golden.Check(t, "testdata/golden/doctor_"+tc.name+".txtar", secs...)
	}
}

// Agent and json carry INFO rows even without -verbose; the human formats
// hide them; a failing run ends in a fail line.
func TestDoctorFormatsAndTrailer(t *testing.T) {
	checks := sampleChecks(true)
	fails, warns := tally(checks)
	emit := func(format string) string {
		p, out, _ := viewPrinter(t, format, 80, false)
		require.NoError(t, p.Emit(core.Result{Cmd: "doctor", View: doctorView(p.Format, checks, false, fails, warns), Failed: true}))
		return out.String()
	}
	require.NotContains(t, emit("txt"), "tool: claude")
	require.Contains(t, emit("agent"), `check name="tool: claude" status=info`)
	require.Contains(t, emit("agent"), "fail cmd=doctor fails=1 warns=1\n")
	require.NotContains(t, emit("agent"), "ok cmd=doctor")
	require.Equal(t, 2, tallyFailsWarns(checks))
}

func tallyFailsWarns(checks []checkResult) int {
	f, w := tally(checks)
	return f + w
}

func TestConfigListViewKeepsTxtLayout(t *testing.T) {
	rows := []configRow{
		{ID: "spinner", Section: "Appearance", Value: "braille", Default: "braille", Source: "default"},
		{ID: "theme", Section: "Appearance", Value: "default", Default: "default", Source: "lua", SetAt: "config.lua:3"},
		{ID: "board_hidden_sections", Section: "Behavior", Value: "", Default: "", Source: "file"},
		{ID: "max_concurrent_sessions", Section: "Behavior", Value: 16, Default: 16, Source: "default"},
	}
	p, out, _ := viewPrinter(t, "txt", 80, false)
	require.NoError(t, p.Emit(core.Result{Cmd: "config", Data: rows, View: configListView(rows)}))
	want := "# Appearance\n" +
		"  spinner                    braille\n" +
		"  theme                      default\n" +
		"\n" +
		"# Behavior\n" +
		"  " + "board_hidden_sections" + "     " + " " + "\n" + // %-26s of the id, a space, an empty value
		"  max_concurrent_sessions    16\n"
	require.Equal(t, want, out.String())
	secs := goldenSections(t, func(p *core.Printer) core.Result {
		return core.Result{Cmd: "config", Data: rows, View: configListView(rows)}
	})
	golden.Check(t, "testdata/golden/config_list.txtar", secs...)
}

func TestConfigValueViews(t *testing.T) {
	for _, f := range []string{"text", "txt", "md", "json", "agent"} {
		p, out, _ := viewPrinter(t, f, 80, false)
		core.SetOut(p)
		require.NoError(t, emitConfigChange("set", "spinner", "moon", "moon"))
		got := out.String()
		switch f {
		case "txt":
			require.Empty(t, got, "txt stays silent")
		case "agent":
			require.Equal(t, "ok cmd=config action=set id=spinner value=moon\n", got)
		case "json":
			require.Equal(t, `{"v":1,"cmd":"config","data":{"id":"spinner","value":"moon"}}`+"\n", got)
		default:
			require.Contains(t, got, "set spinner = moon")
		}
	}
	core.SetOut(nil)
}

func TestSetupAppliedGolden(t *testing.T) {
	rep := tui.DefaultsReport{ConfigPath: "/home/u/.config/rex/config.yaml", RCPath: "/home/u/.zshrc", RCState: "installed",
		AutoTrust: "~/dev", NextCommand: tui.NextCommand("claude", "opus", "high")}
	var all []golden.Section
	for _, f := range []string{"text", "txt", "md", "json", "agent"} {
		p, out, errw := viewPrinter(t, f, 80, false)
		require.NoError(t, emitSetupApplied(p, rep))
		all = append(all, golden.Section{Name: f, Body: out.String() + errw.String()})
	}
	failed := rep
	failed.RCState, failed.RCError = "failed", "permission denied"
	p, out, errw := viewPrinter(t, "txt", 80, false)
	require.NoError(t, emitSetupApplied(p, failed))
	all = append(all, golden.Section{Name: "txt.rc_failed", Body: out.String() + "[stderr]\n" + errw.String()})
	golden.Check(t, "testdata/golden/setup_applied.txtar", all...)
}

func TestNextCommandUsesSingleDashFlags(t *testing.T) {
	require.Equal(t, `rex new -tool claude -model opus -- "your first task"`, tui.NextCommand("claude", "opus", ""))
	require.Equal(t, `rex new -tool claude -model opus -effort high -- "your first task"`, tui.NextCommand("claude", "opus", "high"))
}

func TestUninstallViewsGolden(t *testing.T) {
	plan := &uninstallPlan{
		opts: uninstallOpts{binaries: true, purgeConfig: true},
		home: "/home/u", rexBin: "/home/u/.local/bin/rex", daemonBin: "/home/u/.local/bin/rex-daemon",
		configDir: "/home/u/.config/rex",
	}
	var secs []golden.Section
	for _, f := range []string{"text", "txt", "md", "json", "agent"} {
		for _, mode := range []string{"plan", "dry", "done"} {
			p, out, _ := viewPrinter(t, f, 80, false)
			var r core.Result
			switch mode {
			case "plan":
				r = core.Result{Cmd: "uninstall", Data: plan.data(false, nil), View: plan.planView(false, nil)}
			case "dry":
				r = core.Result{Cmd: "uninstall", Data: plan.data(true, nil), View: plan.planView(true, nil)}
			default:
				done := []removal{{Kind: "binaries", Path: "/home/u/.local/bin/rex"}, {Kind: "config", Path: "/home/u/.config/rex"}}
				r = core.Result{Cmd: "uninstall", Data: plan.data(false, done), View: plan.planView(false, done)}
			}
			require.NoError(t, p.Emit(r))
			secs = append(secs, golden.Section{Name: mode + "." + f, Body: out.String()})
		}
	}
	golden.Check(t, "testdata/golden/uninstall.txtar", secs...)
}

func TestUpdateCheckGolden(t *testing.T) {
	var secs []golden.Section
	for _, f := range []string{"text", "txt", "md", "json", "agent"} {
		p, out, _ := viewPrinter(t, f, 80, false)
		require.NoError(t, runUpdateCheck(p))
		secs = append(secs, golden.Section{Name: f, Body: out.String()})
	}
	golden.Check(t, "testdata/golden/update_check.txtar", secs...)
}

// The update plan is part of the question: stderr, text only.
func TestUpdatePlanIsStderrTextOnly(t *testing.T) {
	for _, f := range []string{"txt", "md", "json", "agent"} {
		p, out, errw := viewPrinter(t, f, 80, false)
		printUpdatePlan(p, "/home/u/go/bin/rex", "", installModeGoInstall, true)
		require.Empty(t, out.String(), f)
		require.Empty(t, errw.String(), f)
	}
	p, out, errw := viewPrinter(t, "text", 80, false)
	printUpdatePlan(p, "/home/u/go/bin/rex", "", installModeGoInstall, true)
	require.Empty(t, out.String())
	require.Contains(t, errw.String(), "go install")
	require.Contains(t, errw.String(), "running (will stop, then restart on next `rex`)")
}

// An update or uninstall without a terminal needs -yes: exit 6, no prompt.
func TestUpdateAndUninstallNeedYesWithoutTerminal(t *testing.T) {
	f := newUpdateFixture()
	installPrinter(false)
	err := runUpdate(nil, updateDeps{
		out: io.Discard, errOut: io.Discard, stdin: bytes.NewReader(nil),
		detectMode: func() (installMode, string, error) { return f.mode, f.exe, nil },
		daemon:     f.daemon.control(),
		goInstall:  func(bool) error { t.Fatal("must not upgrade"); return nil },
	})
	requireExitCode(t, err, core.ExitOperationRefused)
	require.ErrorContains(t, err, "-yes")
	require.True(t, f.daemon.up, "the daemon was not stopped")

	u := newUninstallFixture(t)
	u.stdin = ""
	err = u.run("-binaries=false", "-wipe-state")
	requireExitCode(t, err, core.ExitOperationRefused)
	require.ErrorContains(t, err, "-yes")
	require.Zero(t, u.daemon.stopCount())
	requireExists(t, u.files()...)
}
