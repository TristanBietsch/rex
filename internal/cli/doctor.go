package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/tristanbietsch/rex/internal/daemonctl"
	"github.com/tristanbietsch/rex/internal/settings"
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
	socket := fs.String("socket", DefaultSocket(), "UDS path")
	verbose := fs.Bool("verbose", false, "include INFO-level rows")
	if err := fs.Parse(args); err != nil {
		return NewExitError(ExitInvalidArgs, err.Error())
	}

	checks := runAllChecks(*socket)

	if *asJSON {
		return printDoctorJSON(checks)
	}
	return printDoctorTable(checks, *verbose)
}

// runAllChecks executes every diagnostic check and returns results.
func runAllChecks(socket string) []checkResult {
	var results []checkResult

	results = append(results, checkRexBinary())
	results = append(results, checkDaemonBinary())
	results = append(results, checkDaemonReachable(socket))
	results = append(results, checkDir("config dir", configDirPath()))
	results = append(results, checkDir("state dir", stateDirPath()))
	results = append(results, checkDir("share dir", shareDirPath()))
	results = append(results, checkSettingsLoad())
	results = append(results, checkToolBinaries()...)
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
func checkDaemonReachable(socket string) checkResult {
	if daemonctl.Reachable(socket) {
		return checkResult{"daemon reachable", checkPass, socket}
	}
	return checkResult{"daemon reachable", checkWarn, fmt.Sprintf("socket not responding — run: rex daemon start  (socket=%s)", socket)}
}

// checkDir verifies that a directory exists and is writable.
func checkDir(name, dir string) checkResult {
	if err := os.MkdirAll(dir, 0o755); err != nil {
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
	st := settings.NewStore()
	if err := st.Load(settings.DefaultPath()); err != nil {
		return checkResult{"settings load", checkWarn, fmt.Sprintf("parse error: %v", err)}
	}
	return checkResult{"settings load", checkPass, settings.DefaultPath()}
}

// checkToolBinaries checks each known tool binary via PATH lookup.
func checkToolBinaries() []checkResult {
	tools := []string{"claude", "codex", "gemini", "ollama"}
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

// checkGoVersion reports the runtime Go version.
func checkGoVersion() checkResult {
	return checkResult{"go version", checkInfo, runtime.Version()}
}

// configDirPath returns ~/.config/rex/.
func configDirPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "rex")
}

// stateDirPath returns ~/.local/state/rex/.
func stateDirPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state", "rex")
}

// shareDirPath returns ~/.local/share/rex/sessions/.
func shareDirPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "rex", "sessions")
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
		return NewExitError(ExitGeneric, "")
	}
	return nil
}

// printDoctorTable prints a formatted table to stdout.
func printDoctorTable(checks []checkResult, verbose bool) error {
	var b strings.Builder

	b.WriteString("\n")
	b.WriteString("  " + helpAccent.Render("∴") + " " + helpBold.Render("rex doctor") + "\n")
	b.WriteString("  " + helpHRStyle.Render(strings.Repeat("─", 44)) + "\n\n")

	nameWidth := 0
	for _, c := range checks {
		if w := lipgloss.Width(c.Name); w > nameWidth {
			nameWidth = w
		}
	}
	nameWidth += 2

	var fails, warns int
	for _, c := range checks {
		if c.Status == checkFail {
			fails++
		} else if c.Status == checkWarn {
			warns++
		}
	}

	for _, c := range checks {
		if c.Status == checkInfo && !verbose {
			continue
		}
		sym, styled := renderStatus(c.Status)
		pad := strings.Repeat(" ", nameWidth-lipgloss.Width(c.Name))
		b.WriteString(fmt.Sprintf("  %s %s%s%s\n",
			styled,
			helpCmd.Render(c.Name),
			pad,
			helpDim.Render(c.Detail),
		))
		_ = sym
	}

	b.WriteString("\n")
	b.WriteString("  " + summaryLine(fails, warns) + "\n")
	b.WriteString("\n")

	fmt.Print(b.String())

	if fails > 0 {
		return NewExitError(ExitGeneric, "")
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
		return sym, helpDim.Render(sym)
	}
}

// summaryLine builds the human-readable summary.
func summaryLine(fails, warns int) string {
	if fails == 0 && warns == 0 {
		return helpDim.Render("· all required checks passed")
	}
	if fails == 0 {
		msg := fmt.Sprintf("· all required checks passed, %d warning", warns)
		if warns != 1 {
			msg += "s"
		}
		return helpDim.Render(msg)
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
