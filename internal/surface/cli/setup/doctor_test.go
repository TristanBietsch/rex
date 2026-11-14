package setup

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

func withAgyVersion(t *testing.T, fn func(context.Context) (string, error)) {
	t.Helper()
	old, oldTO := agyVersion, agyVersionTimeout
	agyVersion = fn
	t.Cleanup(func() { agyVersion, agyVersionTimeout = old, oldTO })
}

func TestCheckAgyVersion(t *testing.T) {
	cases := []struct {
		name       string
		out        string
		err        error
		wantStatus checkStatus
		wantDetail string
		wantRows   int
	}{
		{"ok", "agy 1.5.5\n", nil, checkInfo, "agy 1.5.5", 1},
		{"newer", "1.6.0", nil, checkInfo, "agy 1.6.0", 1},
		{"old", "agy 1.4.9", nil, checkWarn, "untested", 1},
		{"not installed", "", exec.ErrNotFound, 0, "", 0},
		{"error", "", errors.New("boom"), checkWarn, "boom", 1},
		{"unparsable", "hello", nil, checkWarn, "hello", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withAgyVersion(t, func(context.Context) (string, error) { return tc.out, tc.err })
			rows := checkAgyVersion()
			require.Len(t, rows, tc.wantRows)
			if tc.wantRows == 0 {
				return
			}
			require.Equal(t, "agy version", rows[0].Name)
			require.Equal(t, tc.wantStatus, rows[0].Status)
			require.Contains(t, rows[0].Detail, tc.wantDetail)
		})
	}
}

func TestCheckAgyVersion_Timeout(t *testing.T) {
	withAgyVersion(t, func(ctx context.Context) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	})
	agyVersionTimeout = 20 * time.Millisecond
	rows := checkAgyVersion()
	require.Len(t, rows, 1)
	require.Equal(t, checkWarn, rows[0].Status)
	require.Contains(t, rows[0].Detail, "timed out")
}

func TestRunAllChecks_IncludesAgyVersion(t *testing.T) {
	withAgyVersion(t, func(context.Context) (string, error) { return "agy 1.5.5", nil })
	t.Setenv("HOME", t.TempDir())
	var names []string
	for _, r := range runAllChecks(&core.SocketFlag{}) {
		names = append(names, r.Name)
	}
	require.Contains(t, names, "agy version")
}

func TestSummaryLine_NoFailNoWarn(t *testing.T) {
	got := summaryLine(0, 0)
	if !strings.Contains(got, "all required checks passed") {
		t.Errorf("unexpected summary: %q", got)
	}
}

func TestSummaryLine_Warns(t *testing.T) {
	got := summaryLine(0, 2)
	if !strings.Contains(got, "2 warnings") {
		t.Errorf("unexpected summary (want '2 warnings'): %q", got)
	}
}

func TestSummaryLine_Fails(t *testing.T) {
	got := summaryLine(1, 0)
	if !strings.Contains(got, "1 failure") {
		t.Errorf("unexpected summary (want '1 failure'): %q", got)
	}
}

func TestSummaryLine_FailsAndWarns(t *testing.T) {
	got := summaryLine(2, 3)
	if !strings.Contains(got, "2 failures") {
		t.Errorf("want '2 failures': %q", got)
	}
	if !strings.Contains(got, "3 warnings") {
		t.Errorf("want '3 warnings': %q", got)
	}
}

func TestCheckStatus_String(t *testing.T) {
	cases := []struct {
		s    checkStatus
		want string
	}{
		{checkPass, "PASS"},
		{checkWarn, "WARN"},
		{checkFail, "FAIL"},
		{checkInfo, "INFO"},
	}
	for _, tc := range cases {
		if got := tc.s.String(); got != tc.want {
			t.Errorf("checkStatus(%d).String() = %q, want %q", tc.s, got, tc.want)
		}
	}
}

func TestStatusWord(t *testing.T) {
	for st, want := range map[checkStatus]string{checkPass: "pass", checkWarn: "warn", checkFail: "fail", checkInfo: "info"} {
		if got := statusWord(st); got != want {
			t.Errorf("statusWord(%v) = %q, want %q", st, got, want)
		}
	}
}

func TestCheckDir_Writable(t *testing.T) {
	dir := t.TempDir()
	r := checkDir("test dir", dir)
	if r.Status != checkPass {
		t.Errorf("expected PASS for writable dir, got %v: %s", r.Status, r.Detail)
	}
}

func TestPrintDoctorJSON_Structure(t *testing.T) {
	checks := []checkResult{
		{"rex binary", checkPass, "/usr/local/bin/rex"},
		{"daemon reachable", checkWarn, "socket not responding"},
		{"missing thing", checkFail, "not found"},
	}

	// Capture via manual JSON marshal (same logic as printDoctorJSON).
	ok := true
	out := doctorOutputJSON{Checks: make([]doctorCheckJSON, 0, len(checks))}
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
		t.Fatalf("marshal: %v", err)
	}

	var decoded doctorOutputJSON
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.OK {
		t.Error("expected ok=false when FAIL check present")
	}
	if len(decoded.Checks) != 3 {
		t.Errorf("expected 3 checks, got %d", len(decoded.Checks))
	}
	if decoded.Checks[1].Status != "WARN" {
		t.Errorf("expected WARN, got %q", decoded.Checks[1].Status)
	}
}

// S-01: doctor created missing config, state and share dirs 0755.
func TestCheckDir_createsOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits")
	}
	dir := filepath.Join(t.TempDir(), "state", "rex")
	r := checkDir("state dir", dir)
	require.Equal(t, checkPass, r.Status, r.Detail)
	for _, d := range []string{dir, filepath.Dir(dir)} {
		info, err := os.Stat(d)
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o700), info.Mode().Perm(), d)
	}
}

// S-01: `rex config edit` created ~/.config/rex 0755. EDITOR=true stands in
// for the user's editor.
func TestConfigEdit_createsConfigDirOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits; no true(1)")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("EDITOR", "true")
	require.NoError(t, configEdit(nil))
	info, err := os.Stat(filepath.Join(home, ".config", "rex"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o700), info.Mode().Perm())
}

// The extra agents (Qwen Code, Cursor CLI, Grok Build) get a tool row like
// the others, by binary name. With none on PATH, "agents installed" fails.
func TestCheckToolBinaries_ListsExtraAgents(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	rows := checkToolBinaries()
	var names []string
	for _, r := range rows[:len(rows)-1] {
		names = append(names, r.Name)
		require.Equal(t, "not found on PATH", r.Detail)
	}
	require.Equal(t, []string{
		"tool: claude", "tool: codex", "tool: agy", "tool: ollama",
		"tool: qwen", "tool: cursor-agent", "tool: grok",
	}, names)
	last := rows[len(rows)-1]
	require.Equal(t, "agents installed", last.Name)
	require.Equal(t, checkFail, last.Status)
}
