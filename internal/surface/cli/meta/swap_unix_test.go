//go:build !windows

package meta

import (
	"errors"
	"slices"
	"testing"

	"github.com/tristanbietsch/rex/internal/surface/tui"
)

// R20 review: on Linux, os.Executable after an install names the replaced
// file "<path> (deleted)"; a swap execs the installed path.
func TestSwapExe(t *testing.T) {
	if got := swapExe("/usr/local/bin/rex (deleted)"); got != "/usr/local/bin/rex" {
		t.Fatalf("swapExe = %q", got)
	}
	if got := swapExe("/usr/local/bin/rex"); got != "/usr/local/bin/rex" {
		t.Fatalf("swapExe = %q", got)
	}
	if startExe == "" && startExeErr == nil {
		t.Fatal("start executable not resolved")
	}
}

func TestSwapEnv(t *testing.T) {
	got := swapEnv([]string{"A=1", tui.SwapEnv + "=old", QuietBootEnv + "=0"}, "/s.json")
	want := []string{"A=1", tui.SwapEnv + "=/s.json", QuietBootEnv + "=1"}
	if !slices.Equal(got, want) {
		t.Fatalf("swapEnv = %v", got)
	}
}

// A swap whose exec fails runs the board again in this build, with the
// same swap state and a notice.
func TestSwapLoopFallsBackWhenExecFails(t *testing.T) {
	var runs []tui.RunOptions
	run := func(o tui.RunOptions) error {
		runs = append(runs, o)
		if len(runs) == 1 {
			return &tui.SwapRequest{Path: "/s.json"}
		}
		return nil
	}
	var execd []string
	err := swapLoop(tui.RunOptions{}, run, func(p string) error { execd = append(execd, p); return errors.New("boom") })
	if err != nil || len(runs) != 2 || !slices.Equal(execd, []string{"/s.json"}) {
		t.Fatalf("err=%v runs=%d execd=%v", err, len(runs), execd)
	}
	if runs[1].SwapState != "/s.json" || runs[1].Notice != "swap failed: boom" || !runs[1].QuietBoot {
		t.Fatalf("second run = %+v", runs[1])
	}
}

func TestExecSwapFailsOnAMissingBinary(t *testing.T) {
	old := startExe
	startExe = "/nonexistent/rex"
	defer func() { startExe = old }()
	if err := execSwap("/s.json"); err == nil {
		t.Fatal("exec of a missing binary must fail")
	}
}
