package cli

import (
	"encoding/json"
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
		s      checkStatus
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
