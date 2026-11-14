package setup

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/runtime/daemonctl"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/runtime/rexlog"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/surface/tui"
	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// onboardSteps is the number of steps in the tour.
const onboardSteps = 5

// onboardTimeout bounds one daemon round trip of the tour.
const onboardTimeout = 3 * time.Second

// Test seams: the daemon and the setup wizard.
var (
	// onboardDaemonTools asks the daemon to rescan PATH and returns its
	// tool list; ok is false when no daemon answers.
	onboardDaemonTools = daemonTools
	// onboardStartDaemon starts the daemon on socket.
	onboardStartDaemon = startDaemon
	// onboardRunSetup runs the rex setup wizard.
	onboardRunSetup = tui.RunSetupWizard
	// onboardInstallChecks is the tour's first step.
	onboardInstallChecks = installChecks
)

// RunOnboard implements `rex onboard`.
func RunOnboard(args []string) error {
	fs := flag.NewFlagSet("onboard", flag.ContinueOnError)
	step := fs.Int("step", 1, "start the tour at step N (1-5)")
	check := fs.Bool("check", false, "run the install and agent checks only, no prompts")
	socketFlag := core.AddSocketFlag(fs)
	out := core.AddOutputFlags(fs, core.AllFormats)
	if _, err := out.Parse(fs, args); err != nil {
		return err
	}
	stepSet := false
	fs.Visit(func(f *flag.Flag) { stepSet = stepSet || f.Name == "step" })
	if *check && stepSet {
		return core.NewExitError(core.ExitInvalidArgs, "onboard: -check and -step do not go together")
	}
	if *step < 1 || *step > onboardSteps {
		return core.Errorf(core.ExitInvalidArgs, "rex onboard -step 1",
			"onboard: -step must be 1 to %d", onboardSteps)
	}
	socket, err := socketFlag.Path()
	if err != nil {
		return err
	}

	rexlog.Init("onboard")
	defer rexlog.Close()
	slog.Info("onboard: started", "step", *step, "check", *check, "socket", socket)

	p := core.Out()
	if *check {
		return runOnboardCheck(p, socket)
	}
	if p.Format != core.FormatText || !p.InTTY || !p.OutTTY {
		return core.Errorf(core.ExitInvalidArgs, "rex onboard -check",
			"onboard: the tour is interactive and needs a terminal (not agent mode, -o or a pipe); use -check for the checks alone").WithID("needs_terminal")
	}
	return newTour(p, socket).run(*step)
}

// onboardAgent is one agent in the -check payload.
type onboardAgent struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Bin       string `json:"bin"`
	Path      string `json:"path,omitempty"`
	Installed bool   `json:"installed"`
	// Offered is false for an extra agent while extra_agents is off.
	Offered bool `json:"offered"`
	// DaemonInstalled is what the daemon sees; absent without a daemon.
	DaemonInstalled *bool  `json:"daemon_installed,omitempty"`
	Hint            string `json:"hint,omitempty"`
}

// onboardData is the -check payload.
type onboardData struct {
	Checks []doctorRow    `json:"checks"`
	Agents []onboardAgent `json:"agents"`
	OK     bool           `json:"ok"`
}

// runOnboardCheck is rex onboard -check: the install checks (without
// starting the daemon) and the agent rows, in any format. Exit 1 when a
// check fails.
func runOnboardCheck(p *core.Printer, socket string) error {
	checks := onboardInstallChecks(socket, false)
	agents := scanAgents()
	daemon, _ := onboardDaemonTools(socket)
	checks = append(checks, agentChecks(agents, daemon)...)
	fails, warns := tally(checks)

	data := onboardData{OK: fails == 0}
	data.Checks = doctorResultData(checks, fails, warns).Checks
	for _, a := range agents {
		oa := onboardAgent{ID: a.ID, Name: a.Name, Bin: a.Bin, Path: a.Path,
			Installed: a.Installed, Offered: !a.Off, Hint: a.Hint}
		if daemon != nil {
			v := daemon[a.ID]
			oa.DaemonInstalled = &v
		}
		data.Agents = append(data.Agents, oa)
	}
	if err := p.Emit(core.Result{
		Cmd:    "onboard",
		Data:   data,
		View:   onboardCheckView(p.Format, checks, fails, warns),
		Failed: fails > 0,
	}); err != nil {
		return err
	}
	if fails > 0 {
		return core.NewExitError(core.ExitGeneric, "")
	}
	return nil
}

// onboardCheckView is the -check report: the doctor layout with every row.
func onboardCheckView(format core.Format, checks []checkResult, fails, warns int) core.View {
	v := doctorView(format, checks, true, fails, warns)
	v[0] = core.Header{Title: "Onboard check", Meta: core.Version + " · " + doctorPlatform}
	return v
}

// installChecks are the tour's first step: the binaries, the daemon and the
// config. With start, a daemon that is not running is started.
func installChecks(socket string, start bool) []checkResult {
	checks := []checkResult{checkRexBinary(), checkDaemonBinary()}
	switch {
	case daemonctl.Reachable(socket):
		checks = append(checks, checkResult{"daemon reachable", checkPass, socket})
	case !start:
		checks = append(checks, checkResult{"daemon reachable", checkWarn,
			"not running; rex starts it with the board (or: rex daemon start)"})
	default:
		if err := onboardStartDaemon(socket); err != nil {
			checks = append(checks, checkResult{"daemon reachable", checkFail, "could not start rex-daemon: " + err.Error()})
		} else {
			checks = append(checks, checkResult{"daemon reachable", checkPass, "started (" + socket + ")"})
		}
	}
	return append(checks, checkSettingsLoad())
}

// agentChecks is one row per known agent and the "agents installed" row.
// daemon is the daemon's installed map (nil without a daemon); an agent the
// CLI finds but the daemon does not means the daemon runs with an older
// PATH.
func agentChecks(agents []agentStatus, daemon map[string]bool) []checkResult {
	out := make([]checkResult, 0, len(agents)+1)
	for _, a := range agents {
		name := a.Name + " (" + a.Bin + ")"
		switch {
		case !a.Installed:
			out = append(out, checkResult{name, checkInfo, "not installed: " + a.Hint})
		case a.Off:
			out = append(out, checkResult{name, checkInfo,
				"installed, not offered while extra_agents is off: rex config set extra_agents true"})
		case daemon != nil && !daemon[a.ID]:
			out = append(out, checkResult{name, checkWarn,
				"the daemon cannot see " + a.Bin + ": it started with an older PATH; run rex daemon restart -cold (asks before it ends running agents)"})
		default:
			out = append(out, checkResult{name, checkPass, a.Path})
		}
	}
	return append(out, checkAgentsInstalled(agents))
}

// daemonTools asks the daemon on socket to rescan PATH and returns which
// tools it sees installed. ok is false when no daemon answers, or one that
// predates RescanTools.
func daemonTools(socket string) (map[string]bool, bool) {
	if !daemonctl.Reachable(socket) {
		return nil, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), onboardTimeout)
	defer cancel()
	c, err := client.DialContext(ctx, socket)
	if err != nil {
		return nil, false
	}
	defer c.Close()
	if _, err := c.HelloContext(ctx, "rex-onboard"); err != nil || c.DaemonVersion() == "" {
		return nil, false
	}
	env, err := c.Do(ctx, protocol.IntentRescanTools, struct{}{})
	if err != nil || env.Type != protocol.EventToolsChanged {
		slog.Warn("onboard: rescan failed", "err", err)
		return nil, false
	}
	var tc protocol.ToolsChanged
	if err := json.Unmarshal(env.Data, &tc); err != nil {
		return nil, false
	}
	out := make(map[string]bool, len(tc.Tools))
	for _, t := range tc.Tools {
		out[t.ID] = t.Installed
	}
	return out, true
}

// startDaemon starts rex-daemon on socket, logging to the daemon log.
func startDaemon(socket string) error {
	log, err := daemonctl.OpenLog()
	if err != nil {
		return err
	}
	defer log.Close()
	_, err = daemonctl.Start(socket, log)
	return err
}

// tour is the interactive rex onboard.
type tour struct {
	p      *core.Printer
	in     *bufio.Reader
	socket string
	// scan is the background agent probe, started before step 1.
	scan chan []agentStatus
	// agents is the last probe's result; scanned says it arrived.
	agents  []agentStatus
	scanned bool
}

func newTour(p *core.Printer, socket string) *tour {
	t := &tour{p: p, in: bufio.NewReader(p.In), socket: socket, scan: make(chan []agentStatus, 1)}
	go func() { t.scan <- scanAgents() }()
	return t
}

// tourStep renders one step and reports whether it blocks the tour (a
// failed check that must be fixed first).
type tourStep struct {
	title  string
	render func(t *tour) (blocked bool)
	// keys are the step's own keys, shown in the prompt.
	keys string
	// do handles one of keys; it returns true to show the step again.
	do func(t *tour, key string) bool
}

var tourSteps = [onboardSteps]tourStep{
	{title: "Check install", render: (*tour).stepInstall, keys: "r recheck",
		do: func(_ *tour, key string) bool { return key == "r" }},
	{title: "Coding agents", render: (*tour).stepAgents, keys: "r rescan",
		do: func(t *tour, key string) bool {
			if key != "r" {
				return false
			}
			t.agents, t.scanned = scanAgents(), true
			return true
		}},
	{title: "Configure", render: (*tour).stepConfig, keys: "s run rex setup",
		do: (*tour).runSetup},
	{title: "The board", render: (*tour).stepBoard},
	{title: "Next steps", render: (*tour).stepNext},
}

// run walks the steps from start. q stops (exit 1 when the step shown has a
// failed check), b goes back, enter goes on.
func (t *tour) run(start int) error {
	i := start - 1
	for i < onboardSteps {
		s := tourSteps[i]
		t.printf("\n[%d/%d] %s\n\n", i+1, onboardSteps, s.title)
		blocked := s.render(t)
		key := t.prompt(i, s, blocked)
		switch {
		case key == "q":
			t.printf("resume with: rex onboard -step %d\n", i+1)
			if blocked {
				return core.NewExitError(core.ExitGeneric, "")
			}
			return nil
		case key == "b":
			if i > 0 {
				i--
			}
		case key != "" && s.do != nil && s.do(t, key):
			// The step's own key: show it again.
		case key == "" && blocked:
			t.printf("fix the failed check first (or q to quit)\n")
		case key == "":
			i++
		default:
			t.printf("unknown key %q\n", key)
		}
	}
	if err := markOnboarded(); err != nil {
		slog.Warn("onboard: marker not written", "err", err)
	}
	return nil
}

// prompt asks for the next key and returns it lowercased; "" is enter. At
// the end of input it answers q.
func (t *tour) prompt(i int, s tourStep, blocked bool) string {
	parts := []string{"enter next"}
	if i == onboardSteps-1 {
		parts[0] = "enter finish"
	}
	if blocked {
		parts = parts[1:]
	}
	if s.keys != "" {
		parts = append(parts, s.keys)
	}
	if i > 0 {
		parts = append(parts, "b back")
	}
	parts = append(parts, "q quit")
	t.printf("\n%s › ", strings.Join(parts, " · "))
	line, err := t.in.ReadString('\n')
	if err != nil && line == "" {
		t.printf("\n")
		return "q"
	}
	return strings.ToLower(strings.TrimSpace(line))
}

func (t *tour) printf(format string, a ...any) { _, _ = fmt.Fprintf(t.p.Out, format, a...) }

// emit renders v in the text format.
func (t *tour) emit(v core.View) { _ = t.p.Emit(core.Result{Cmd: "onboard", View: v}) }

func (t *tour) emitChecks(checks []checkResult) (fails int) {
	rows := make([]core.Check, len(checks))
	for i, c := range checks {
		rows[i] = core.Check{Name: c.Name, Status: statusWord(c.Status), Detail: c.Detail}
	}
	fails, _ = tally(checks)
	t.emit(core.View{core.Checks{Rows: rows}})
	return fails
}

func (t *tour) stepInstall() bool {
	t.printf("rex is a board for coding agents: one daemon keeps every agent\nrunning, the board and the CLI talk to it.\n\n")
	return t.emitChecks(onboardInstallChecks(t.socket, true)) > 0
}

// agentList is the background probe's result, waiting for it the first
// time (the tour may start past step 2).
func (t *tour) agentList() []agentStatus {
	if !t.scanned {
		t.agents, t.scanned = <-t.scan, true
	}
	return t.agents
}

func (t *tour) stepAgents() bool {
	t.agentList()
	daemon, _ := onboardDaemonTools(t.socket)
	t.printf("rex starts the coding agents you have installed. The new-agent page\n(n on the board) offers only these; one you install later shows up\nwithin 30 seconds.\n\n")
	if t.emitChecks(agentChecks(t.agents, daemon)) > 0 {
		t.printf("\nrex needs at least one coding agent. Install one of the above, then\npress r. rex never installs an agent itself.\n")
		return true
	}
	return false
}

func (t *tour) stepConfig() bool {
	st, path, _ := settings.LoadDefault()
	if !configExists(path) {
		t.printf("No config yet. Press s to run rex setup: the default agent, model\nand effort, and the shell integration. enter skips it; rex works\nwith its defaults.\n")
		return false
	}
	tool := st.String("default_spawn_tool")
	t.emit(core.View{core.KV{Pairs: []core.Pair{
		{Key: "config", Label: "Config", Val: core.Cell{Text: path}},
		{Key: "tool", Label: "Default agent", Val: core.Cell{Text: orDash(tool)}},
		{Key: "model", Label: "Default model", Val: core.Cell{Text: orDash(st.String("default_spawn_model"))}},
		{Key: "effort", Label: "Default effort", Val: core.Cell{Text: orDash(st.String("default_spawn_effort"))}},
	}}})
	if tool != "" && !t.usable(tool) {
		t.printf("\n! the default agent %q is not installed. Pick one that is:\n", tool)
		for _, a := range t.agentList() {
			if a.Installed && !a.Off {
				t.printf("    rex config set default_spawn_tool %s\n", a.ID)
			}
		}
	}
	t.printf("\nPress s to run rex setup again.\n")
	return false
}

// usable reports whether agent id is installed and offered.
func (t *tour) usable(id string) bool {
	for _, a := range t.agentList() {
		if a.ID == id {
			return a.Installed && !a.Off
		}
	}
	return false
}

func (t *tour) runSetup(key string) bool {
	if key != "s" {
		return false
	}
	_, path, err := settings.LoadDefault()
	if path == "" {
		t.printf("setup: %v\n", err)
		return true
	}
	if err := onboardRunSetup(path, t.socket, ""); err != nil {
		t.printf("setup: %v\n", err)
	}
	return true
}

// tourKeys are the board keys the tour teaches, by their help label; the
// descriptions come from the board's key table.
var tourKeys = []string{"n", "enter", "qq / ctrl+]", "tab", "cc", "uc", "S", "?"}

func (t *tour) stepBoard() bool {
	t.printf("Run rex to open the board. Each agent is a row in one of four\nsections: READY needs you, WAITING waits on a tool, WORKING is busy,\nDONE is finished. Closing the board stops nothing.\n\n")
	items := make([]core.Def, 0, len(tourKeys))
	for _, k := range tourKeys {
		if desc, ok := tui.BoardKeyDesc(k); ok {
			items = append(items, core.Def{Name: k, Desc: desc})
		}
	}
	t.emit(core.View{core.Defs{Kind: "key", Items: items}})
	return false
}

func (t *tour) stepNext() bool {
	t.emit(core.View{core.Defs{Kind: "command", Items: []core.Def{
		{Name: "rex", Desc: "open the board and start an agent with n"},
		{Name: "rex onboard -check", Desc: "check the install and the agents again"},
		{Name: "rex doctor", Desc: "every install check"},
		{Name: "rex --help", Desc: "every command"},
	}}})
	t.printf("\nA test agent that needs no API key: docs/quickstart.md\n")
	return false
}

// configExists reports whether path is a config file with something in it.
func configExists(path string) bool {
	b, err := os.ReadFile(path)
	return err == nil && len(strings.TrimSpace(string(b))) > 0
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// markOnboarded writes the onboarded marker (paths.OnboardedFile).
func markOnboarded() error {
	path, err := paths.OnboardedFile()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strconv.FormatInt(time.Now().Unix(), 10)+"\n"), 0o600)
}

// NeedsOnboarding reports whether the board should suggest rex onboard: no
// config file and the tour never finished.
func NeedsOnboarding() bool {
	if path, err := paths.ConfigFile(); err != nil || configExists(path) {
		return false
	}
	path, err := paths.OnboardedFile()
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err != nil
}
