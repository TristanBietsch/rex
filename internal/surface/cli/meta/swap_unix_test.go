//go:build !windows

package meta

import "testing"

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
