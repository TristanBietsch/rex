//go:build windows

package daemonctl

import (
	"os"

	"github.com/tristanbietsch/rex/internal/daemon/ownenv"
)

// daemonEnv is the allowlisted environment the daemon starts with. Windows
// has no exec-in-place, so the daemon cannot filter itself after it starts:
// the environment is built here, before the process exists. nil (the
// caller's environment) when child_env is inherit.
func daemonEnv() []string {
	env, needExec := ownenv.FilteredEnv(os.Environ(), nil)
	if !needExec {
		return nil
	}
	return env
}
