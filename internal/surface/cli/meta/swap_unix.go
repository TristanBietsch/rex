//go:build !windows

package meta

import (
	"os"
	"strings"
	"syscall"

	"github.com/tristanbietsch/rex/internal/surface/tui"
)

// startExe is this program's path as resolved at start. A swap execs it
// again (the installed path): asked after an install replaced the file,
// Linux would answer "<path> (deleted)".
var startExe, startExeErr = os.Executable()

// swapExe is the path a swap execs: startExe without a " (deleted)" suffix.
func swapExe(exe string) string { return strings.TrimSuffix(exe, " (deleted)") }

// execSwap replaces this board with the installed rex binary (same pid),
// silenced (QuietBootEnv) and pointed at the swap state. It returns only
// on failure.
func execSwap(statePath string) error {
	if startExeErr != nil {
		return startExeErr
	}
	exe := swapExe(startExe)
	env := make([]string, 0, len(os.Environ())+2)
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, tui.SwapEnv+"=") || strings.HasPrefix(kv, QuietBootEnv+"=") {
			continue
		}
		env = append(env, kv)
	}
	env = append(env, tui.SwapEnv+"="+statePath, QuietBootEnv+"=1")
	return syscall.Exec(exe, os.Args, env) //nolint:gosec // G204: this program's own executable.
}
