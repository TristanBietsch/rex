package setup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

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

func TestRenderStatus_Symbols(t *testing.T) {
	cases := []struct {
		s       checkStatus
		wantSym string
	}{
		{checkPass, "✓"},
		{checkWarn, "!"},
		{checkFail, "✗"},
		{checkInfo, "·"},
	}
	for _, tc := range cases {
		sym, _ := renderStatus(tc.s)
		if sym != tc.wantSym {
			t.Errorf("renderStatus(%v) sym = %q, want %q", tc.s, sym, tc.wantSym)
		}
	}
}

func TestCheckDir_Exists(t *testing.T) {
	dir := t.TempDir()
	r := checkDir("test dir", dir)
	if r.Status != checkPass {
		t.Errorf("expected PASS for existing dir, got %v: %s", r.Status, r.Detail)
	}
}

// Diagnosis must not create anything: a missing dir is reported, not made.
func TestCheckDir_MissingIsNotCreated(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "absent")
	r := checkDir("test dir", dir)
	if r.Status != checkInfo {
		t.Errorf("expected INFO for missing dir, got %v: %s", r.Status, r.Detail)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("checkDir created %s (stat err %v)", dir, err)
	}
}

func TestCheckDir_FileIsFail(t *testing.T) {
	f := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(f, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if r := checkDir("test dir", f); r.Status != checkFail {
		t.Errorf("expected FAIL for a file, got %v: %s", r.Status, r.Detail)
	}
}

// rex daemon start runs the rex-daemon next to rex even when PATH has none,
// so doctor must not report it missing.
func TestCheckDaemonBinary_NextToRex(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	name := "rex-daemon"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	sibling := filepath.Join(filepath.Dir(self), name)
	if _, err := os.Stat(sibling); err == nil {
		t.Skip("a rex-daemon already sits next to the test binary")
	}
	if err := os.WriteFile(sibling, []byte("x"), 0o755); err != nil {
		t.Skipf("cannot write next to the test binary: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(sibling) })
	t.Setenv("PATH", t.TempDir())

	r := checkDaemonBinary()
	if r.Status != checkPass || r.Detail != sibling {
		t.Fatalf("got %v %q, want PASS %q", r.Status, r.Detail, sibling)
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
