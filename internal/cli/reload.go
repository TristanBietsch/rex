package cli

import (
	"flag"
	"fmt"
	"os/exec"
	"strings"
)

// RunReload sends SIGHUP to the running rex-daemon so the user/script can
// trigger a config re-read.
func RunReload(args []string) error {
	fs := flag.NewFlagSet("reload", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return NewExitError(ExitInvalidArgs, err.Error())
	}
	out, err := exec.Command("pgrep", "rex-daemon").Output()
	if err != nil {
		return NewExitError(ExitDaemonUnreachable, "no rex-daemon process found")
	}
	pid := strings.TrimSpace(strings.Split(string(out), "\n")[0])
	if pid == "" {
		return NewExitError(ExitDaemonUnreachable, "no rex-daemon process found")
	}
	if err := exec.Command("kill", "-HUP", pid).Run(); err != nil {
		return NewExitError(ExitGeneric, fmt.Sprintf("kill -HUP %s: %v", pid, err))
	}
	return nil
}
