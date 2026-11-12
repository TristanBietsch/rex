//go:build !windows

package meta

import (
	"os"
	"strings"
	"syscall"

	"github.com/tristanbietsch/rex/internal/surface/tui"
)

// execSwap replaces this board with the installed rex binary (same pid),
// silenced (QuietBootEnv) and pointed at the swap state. It returns only
// on failure.
func execSwap(statePath string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
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
