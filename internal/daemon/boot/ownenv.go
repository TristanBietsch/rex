//go:build !windows

package boot

import (
	"fmt"
	"os"

	"github.com/tristanbietsch/rex/internal/daemon/ownenv"
)

// ReexecFiltered re-executes the daemon in place (same pid) with its filtered
// environment (ownenv.FilteredEnv), so the keys exported in the shell that
// started rex are no longer in the daemon's memory or its /proc/<pid>/environ.
// It does nothing when the environment is already filtered or child_env is
// inherit. exec is syscall.Exec in the daemon and a fake in tests; it is
// called at most once, with os.Executable() and os.Args. An error from exec is
// returned.
func ReexecFiltered(exec func(argv0 string, argv []string, env []string) error) error {
	env, needExec := ownenv.FilteredEnv(os.Environ(), os.Args[1:])
	if !needExec {
		return nil
	}
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("filter environment: %w", err)
	}
	if err := exec(self, os.Args, env); err != nil {
		return fmt.Errorf("filter environment: re-exec: %w", err)
	}
	return nil
}
