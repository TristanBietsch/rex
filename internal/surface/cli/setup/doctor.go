package setup

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/runtime/daemonctl"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

// checkStatus represents the severity of a diagnostic check result.
type checkStatus int

const (
	checkPass checkStatus = iota
	checkWarn
	checkFail
	checkInfo
)

func (s checkStatus) String() string {
	switch s {
	case checkPass:
		return "PASS"
	case checkWarn:
		return "WARN"
	case checkFail:
		return "FAIL"
	case checkInfo:
		return "INFO"
	default:
		return "UNKN"
	}
}

// checkResult is a single diagnostic check result.
type checkResult struct {
	Name   string
	Status checkStatus
	Detail string
}

// doctorCheckJSON is the JSON representation of a check result.
type doctorCheckJSON struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

// doctorOutputJSON is the top-level JSON output.
type doctorOutputJSON struct {
	Checks []doctorCheckJSON `json:"checks"`
	OK     bool              `json:"ok"`
}

var (
	doctorPassStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#4ADE80")) // green
	doctorWarnStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FACC15")) // yellow
	doctorFailStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#F87171")) // red
	doctorInfoStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#7A7F8C")) // dim
)

// RunDoctor runs all diagnostic checks and prints a formatted report.
func RunDoctor(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "output JSON")
	socket := core.AddSocketFlag(fs)
	verbose := fs.Bool("verbose", false, "include INFO-level rows")
	_, err := core.ParseArgs(fs, args)
	if err != nil {
		return err
	}

	checks := runAllChecks(socket)

	if *asJSON {
		return printDoctorJSON(checks)
	}
	return printDoctorTable(checks, *verbose)
}

// runAllChecks executes every diagnostic check and returns results.
func runAllChecks(socket *core.SocketFlag) []checkResult {
	var results []checkResult

	results = append(results, checkRexBinary())
	results = append(results, checkDaemonBinary())
	results = append(results, checkDaemonReachable(socket))
	results = append(results, checkRexDirs()...)
	results = append(results, checkSettingsLoad())
	results = append(results, checkToolBinaries()...)
	results = append(results, checkSnapshots()...)
	results = append(results, checkAgyVersion()...)
	results = append(results, checkGoVersion())

	return results
}

// checkRexBinary checks that rex is on PATH and matches the running executable.
func checkRexBinary() checkResult {
	self, err := os.Executable()
	if err != nil {
		slog.Error("doctor: os.Executable failed", "err", err)
		return checkResult{"rex binary", checkFail, fmt.Sprintf("could not determine executable path: %v", err)}
	}

	pathBin, err := exec.LookPath("rex")
	if err != nil {
		return checkResult{"rex binary", checkWarn, fmt.Sprintf("not found on PATH (running from %s)", self)}
	}

	// Resolve symlinks for comparison.
	selfResolved, err1 := filepath.EvalSymlinks(self)
	pathResolved, err2 := filepath.EvalSymlinks(pathBin)
	if err1 != nil || err2 != nil {
		// Fall back to raw comparison.
		selfResolved = self
		pathResolved = pathBin
	}

	if selfResolved == pathResolved {
		return checkResult{"rex binary", checkPass, pathBin}
	}
	return checkResult{"rex binary", checkWarn, fmt.Sprintf("PATH has %s but running %s", pathBin, self)}
}

// checkDaemonBinary checks that rex-daemon is on PATH.
func checkDaemonBinary() checkResult {
	path, err := exec.LookPath("rex-daemon")
	if err != nil {
		return checkResult{"rex-daemon binary", checkFail, "not found on PATH"}
	}
	return checkResult{"rex-daemon binary", checkPass, path}
}

// checkDaemonReachable checks that the daemon socket is answering.
func checkDaemonReachable(flag *core.SocketFlag) checkResult {
	socket, err := flag.Path()
	if err != nil {
		return checkResult{"daemon reachable", checkFail, err.Error()}
	}
	if daemonctl.Reachable(socket) {
		return checkResult{"daemon reachable", checkPass, socket}
	}
	return checkResult{"daemon reachable", checkWarn, fmt.Sprintf("socket not responding — run: rex daemon start  (socket=%s)", socket)}
}

// checkDir verifies that a directory exists and is writable, creating a
// missing one (and its parents) owner-only.
func checkDir(name, dir string) checkResult {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return checkResult{name, checkFail, fmt.Sprintf("cannot create %s: %v", dir, err)}
	}

	// Write-test via a temp file.
	tmp, err := os.CreateTemp(dir, ".rex-doctor-*")
	if err != nil {
		return checkResult{name, checkFail, fmt.Sprintf("not writable (%s): %v", dir, err)}
	}
	_ = tmp.Close()
	_ = os.Remove(tmp.Name())

	return checkResult{name, checkPass, dir}
}

// checkSettingsLoad attempts to load settings from the default path.
func checkSettingsLoad() checkResult {
	_, path, err := settings.LoadDefault()
	switch {
	case path == "":
		return checkResult{"settings load", checkFail, err.Error()}
	case err != nil:
		return checkResult{"settings load", checkWarn, fmt.Sprintf("parse error: %v", err)}
	}
	return checkResult{"settings load", checkPass, path}
}

// checkToolBinaries checks each known tool binary via PATH lookup.
func checkToolBinaries() []checkResult {
	tools := []string{"claude", "codex", "agy", "ollama"}
	var results []checkResult
	for _, tool := range tools {
		path, err := exec.LookPath(tool)
		if err != nil {
			results = append(results, checkResult{fmt.Sprintf("tool: %s", tool), checkInfo, "not found on PATH"})
		} else {
			results = append(results, checkResult{fmt.Sprintf("tool: %s", tool), checkInfo, path})
		}
	}
	return results
}

// agyMinVersion is the oldest agy rex was tested against.
var agyMinVersion = [3]int{1, 5, 5}

// agyVersionTimeout bounds `agy --version`.
var agyVersionTimeout = 2 * time.Second

// agyVersion returns the raw output of `agy --version`. Tests replace it so
// the real agy is never run.
var agyVersion = func(ctx context.Context) (string, error) {
	path, err := exec.LookPath("agy")
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, path, "--version")
	// A child that keeps stdout open must not outlast the timeout.
	cmd.WaitDelay = 500 * time.Millisecond
	out, err := cmd.Output()
	return string(out), err
}

var semverRE = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`)

// parseSemver pulls the first major.minor.patch triple out of s.
func parseSemver(s string) ([3]int, bool) {
	m := semverRE.FindStringSubmatch(s)
	if m == nil {
		return [3]int{}, false
	}
	var v [3]int
	for i := range v {
		n, err := strconv.Atoi(m[i+1])
		if err != nil {
			return [3]int{}, false
		}
		v[i] = n
	}
	return v, true
}

func semverLess(a, b [3]int) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

// checkAgyVersion reports the installed agy version. It returns no row when
// agy is not installed.
func checkAgyVersion() []checkResult {
	const name = "agy version"
	ctx, cancel := context.WithTimeout(context.Background(), agyVersionTimeout)
	defer cancel()
	out, err := agyVersion(ctx)
	switch {
	case errors.Is(err, exec.ErrNotFound):
		return nil
	case errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil:
		return []checkResult{{name, checkWarn, fmt.Sprintf("`agy --version` timed out after %s", agyVersionTimeout)}}
	case err != nil:
		return []checkResult{{name, checkWarn, fmt.Sprintf("`agy --version` failed: %v", err)}}
	}
	out = strings.TrimSpace(out)
	v, ok := parseSemver(out)
	if !ok {
		return []checkResult{{name, checkWarn, fmt.Sprintf("unrecognized `agy --version` output: %q", out)}}
	}
	ver := fmt.Sprintf("%d.%d.%d", v[0], v[1], v[2])
	if semverLess(v, agyMinVersion) {
		return []checkResult{{name, checkWarn, fmt.Sprintf("agy %s (untested; rex expects %d.%d.%d or newer)", ver, agyMinVersion[0], agyMinVersion[1], agyMinVersion[2])}}
	}
	return []checkResult{{name, checkInfo, "agy " + ver}}
}

// checkGoVersion reports the runtime Go version.
func checkGoVersion() checkResult {
	return checkResult{"go version", checkInfo, runtime.Version()}
}

// checkRexDirs checks rex's config, state and session dirs (see package
// paths). Without a home directory they cannot be located, which fails the
// check instead of testing dirs relative to the working directory.
func checkRexDirs() []checkResult {
	l, err := paths.Resolve()
	if err != nil {
		return []checkResult{{"rex dirs", checkFail, err.Error()}}
	}
	return []checkResult{
		checkDir("config dir", l.ConfigDir),
		checkDir("state dir", l.LogDir),
		checkDir("share dir", l.SessionsDir),
	}
}

// printDoctorJSON prints all checks as a single JSON line.
func printDoctorJSON(checks []checkResult) error {
	ok := true
	out := doctorOutputJSON{
		Checks: make([]doctorCheckJSON, 0, len(checks)),
	}
	for _, c := range checks {
		if c.Status == checkFail {
			ok = false
		}
		out.Checks = append(out.Checks, doctorCheckJSON{
			Name:   c.Name,
			Status: c.Status.String(),
			Detail: c.Detail,
		})
	}
	out.OK = ok

	b, err := json.Marshal(out)
	if err != nil {
		return fmt.Errorf("doctor: marshal JSON: %w", err)
	}
	fmt.Println(string(b))
	if !ok {
		return core.NewExitError(core.ExitGeneric, "")
	}
	return nil
}

// printDoctorTable prints a formatted table to stdout.
func printDoctorTable(checks []checkResult, verbose bool) error {
	var b strings.Builder

	b.WriteString("\n")
	b.WriteString("  " + core.HelpAccent.Render("∴") + " " + core.HelpBold.Render("rex doctor") + "\n")
	b.WriteString("  " + core.HelpHRStyle.Render(strings.Repeat("─", 44)) + "\n\n")

	nameWidth := 0
	for _, c := range checks {
		if w := lipgloss.Width(c.Name); w > nameWidth {
			nameWidth = w
		}
	}
	nameWidth += 2

	var fails, warns int
	for _, c := range checks {
		switch c.Status {
		case checkFail:
			fails++
		case checkWarn:
			warns++
		case checkPass, checkInfo:
			// Not tallied: the summary line reports only fails and warns.
		}
	}

	for _, c := range checks {
		if c.Status == checkInfo && !verbose {
			continue
		}
		sym, styled := renderStatus(c.Status)
		pad := strings.Repeat(" ", nameWidth-lipgloss.Width(c.Name))
		fmt.Fprintf(&b, "  %s %s%s%s\n",
			styled,
			core.HelpCmd.Render(c.Name),
			pad,
			core.HelpDim.Render(c.Detail),
		)
		_ = sym
	}

	b.WriteString("\n")
	b.WriteString("  " + summaryLine(fails, warns) + "\n")
	b.WriteString("\n")

	fmt.Print(b.String())

	if fails > 0 {
		return core.NewExitError(core.ExitGeneric, "")
	}
	return nil
}

// renderStatus returns the symbol string and a lipgloss-styled version.
func renderStatus(s checkStatus) (string, string) {
	switch s {
	case checkPass:
		sym := "✓"
		return sym, doctorPassStyle.Render(sym)
	case checkWarn:
		sym := "!"
		return sym, doctorWarnStyle.Render(sym)
	case checkFail:
		sym := "✗"
		return sym, doctorFailStyle.Render(sym)
	case checkInfo:
		sym := "·"
		return sym, doctorInfoStyle.Render(sym)
	default:
		sym := "?"
		return sym, core.HelpDim.Render(sym)
	}
}

// summaryLine builds the human-readable summary.
func summaryLine(fails, warns int) string {
	if fails == 0 && warns == 0 {
		return core.HelpDim.Render("· all required checks passed")
	}
	if fails == 0 {
		msg := fmt.Sprintf("· all required checks passed, %d warning", warns)
		if warns != 1 {
			msg += "s"
		}
		return core.HelpDim.Render(msg)
	}
	msg := fmt.Sprintf("· %d failure", fails)
	if fails != 1 {
		msg += "s"
	}
	if warns > 0 {
		msg += fmt.Sprintf(", %d warning", warns)
		if warns != 1 {
			msg += "s"
		}
	}
	return doctorFailStyle.Render(msg)
}
