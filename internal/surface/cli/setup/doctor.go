package setup

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

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

// RunDoctor runs all diagnostic checks and prints the report. It exits 1 when
// a check fails; the report itself is the answer, so nothing more is printed.
func RunDoctor(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	out := core.AddOutputFlags(fs, core.AllFormats, core.WithLegacyJSON())
	socket := core.AddSocketFlag(fs)
	verbose := fs.Bool("verbose", false, "include INFO-level rows")
	_, err := out.Parse(fs, args)
	if err != nil {
		return err
	}

	checks := runAllChecks(socket)
	fails, warns := tally(checks)

	p := core.Out()
	if err := p.Emit(core.Result{
		Cmd:    "doctor",
		Data:   doctorResultData(checks, fails, warns),
		View:   doctorView(p.Format, checks, *verbose, fails, warns),
		Failed: fails > 0,
		Legacy: func(w io.Writer) error { return printDoctorJSON(w, checks) },
	}); err != nil {
		return err
	}
	if fails > 0 {
		return core.NewExitError(core.ExitGeneric, "")
	}
	return nil
}

// runAllChecks executes every diagnostic check and returns results.
func runAllChecks(socket *core.SocketFlag) []checkResult {
	var results []checkResult

	results = append(results, checkRexBinary())
	results = append(results, checkDaemonBinary())
	results = append(results, checkDaemonReachable(socket))
	results = append(results, checkRexDirs()...)
	results = append(results, checkSettingsLoad())
	results = append(results, checkConfigLua())
	results = append(results, checkToolBinaries()...)
	results = append(results, checkSnapshots()...)
	results = append(results, checkAgyVersion()...)
	results = append(results, checkGoVersion())
	results = append(results, checkCLIErrors(time.Now()))

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

// checkConfigLua reports config.lua: off, absent, failed, refused something,
// or how many settings and themes it set. All text is flattened.
func checkConfigLua() checkResult {
	const name = "config.lua"
	s, rep, _, err := settings.LoadDefaultReport()
	switch {
	case err != nil && s == nil:
		return checkResult{name, checkInfo, "settings not loaded"}
	case !settings.LuaConfigOn(s):
		return checkResult{name, checkInfo, "off (rex config set lua_config true)"}
	case !rep.Exists:
		return checkResult{name, checkInfo, "none (rex config edit -lua)"}
	case rep.Err != nil:
		return checkResult{name, checkWarn, rep.Err.Error() + luaKeptSuffix(rep)}
	case len(rep.Warnings) > 0:
		return checkResult{name, checkWarn, fmt.Sprintf("%d warning(s), first: %s", len(rep.Warnings), rep.Warnings[0])}
	case len(rep.ThemeIssues) > 0:
		return checkResult{name, checkWarn, fmt.Sprintf("%d theme issue(s), first: %s", len(rep.ThemeIssues), rep.ThemeIssues[0])}
	}
	return checkResult{name, checkPass, fmt.Sprintf("%d setting(s), %d theme(s)", len(rep.Values), len(rep.Themes))}
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

// checkToolBinaries checks each known tool binary via PATH lookup, the extra
// agents (Qwen Code, Cursor CLI, Grok Build) included.
func checkToolBinaries() []checkResult {
	tools := []string{"claude", "codex", "agy", "ollama", "qwen", "cursor-agent", "grok"}
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

// checkCLIErrors reports failed rex commands logged in the last 24 hours (see
// core.RecordError): an info row when there are none, a warning with the
// count and the command that shows them otherwise.
func checkCLIErrors(now time.Time) checkResult {
	const name = "cli errors"
	path, err := core.ErrLogPath()
	if err != nil {
		return checkResult{name, checkInfo, "no log folder: " + err.Error()}
	}
	recs, err := core.ReadErrLog(path)
	if err != nil {
		return checkResult{name, checkWarn, fmt.Sprintf("cannot read %s: %v", path, err)}
	}
	n := 0
	for _, r := range recs {
		if t, err := time.Parse(time.RFC3339, r.Time); err == nil && now.Sub(t) <= 24*time.Hour {
			n++
		}
	}
	if n == 0 {
		return checkResult{name, checkInfo, "none in 24h"}
	}
	return checkResult{name, checkWarn, fmt.Sprintf("%d in 24h · rex errors", n)}
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

// tally counts failed and warned checks.
func tally(checks []checkResult) (fails, warns int) {
	for _, c := range checks {
		switch c.Status {
		case checkFail:
			fails++
		case checkWarn:
			warns++
		case checkPass, checkInfo:
			// Not tallied: the summary reports only fails and warns.
		}
	}
	return fails, warns
}

// printDoctorJSON writes the legacy -json document: every check, INFO rows
// included, as one line.
func printDoctorJSON(w io.Writer, checks []checkResult) error {
	out := doctorOutputJSON{Checks: make([]doctorCheckJSON, 0, len(checks)), OK: true}
	for _, c := range checks {
		if c.Status == checkFail {
			out.OK = false
		}
		out.Checks = append(out.Checks, doctorCheckJSON{Name: c.Name, Status: c.Status.String(), Detail: c.Detail})
	}
	b, err := json.Marshal(out)
	if err != nil {
		return fmt.Errorf("doctor: marshal JSON: %w", err)
	}
	_, err = fmt.Fprintln(w, string(b))
	return err
}

// doctorData is the `-o json` payload of rex doctor.
type doctorData struct {
	Checks []doctorRow `json:"checks"`
	OK     bool        `json:"ok"`
	Fails  int         `json:"fails"`
	Warns  int         `json:"warns"`
}

type doctorRow struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

func statusWord(s checkStatus) string {
	switch s {
	case checkPass:
		return "pass"
	case checkWarn:
		return "warn"
	case checkFail:
		return "fail"
	case checkInfo:
		return "info"
	}
	return "info"
}

func doctorResultData(checks []checkResult, fails, warns int) doctorData {
	rows := make([]doctorRow, len(checks))
	for i, c := range checks {
		rows[i] = doctorRow{Name: c.Name, Status: statusWord(c.Status), Detail: c.Detail}
	}
	return doctorData{Checks: rows, OK: fails == 0, Fails: fails, Warns: warns}
}

// doctorPlatform is shown in the report header; tests pin it.
var doctorPlatform = runtime.GOOS + "/" + runtime.GOARCH

// doctorView renders the checks as a Checks block. The human formats hide
// INFO rows unless -verbose; agent and json always carry them.
func doctorView(format core.Format, checks []checkResult, verbose bool, fails, warns int) core.View {
	human := format == core.FormatText || format == core.FormatTXT || format == core.FormatMD
	rows := make([]core.Check, 0, len(checks))
	for _, c := range checks {
		if c.Status == checkInfo && human && !verbose {
			continue
		}
		rows = append(rows, core.Check{Name: c.Name, Status: statusWord(c.Status), Detail: c.Detail})
	}
	tone := core.ToneNone
	switch {
	case fails > 0:
		tone = core.ToneErr
	case warns > 0:
		tone = core.ToneWarn
	}
	return core.View{
		core.Header{Title: "Doctor", Meta: core.Version + " · " + doctorPlatform},
		core.Checks{Rows: rows},
		core.Note{Tone: tone, Text: summaryLine(fails, warns)},
		core.KV{Pairs: []core.Pair{
			{Key: "fails", Val: core.Cell{Text: fmt.Sprint(fails), Raw: fails}, Formats: core.SetOf(core.FormatAgent)},
			{Key: "warns", Val: core.Cell{Text: fmt.Sprint(warns), Raw: warns}, Formats: core.SetOf(core.FormatAgent)},
		}},
	}
}

// summaryLine builds the human-readable summary.
func summaryLine(fails, warns int) string {
	plural := func(n int, word string) string {
		if n == 1 {
			return fmt.Sprintf("%d %s", n, word)
		}
		return fmt.Sprintf("%d %ss", n, word)
	}
	switch {
	case fails == 0 && warns == 0:
		return "all required checks passed"
	case fails == 0:
		return "all required checks passed, " + plural(warns, "warning")
	case warns > 0:
		return plural(fails, "failure") + ", " + plural(warns, "warning")
	}
	return plural(fails, "failure")
}
