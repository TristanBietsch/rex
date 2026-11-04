//go:build !windows

package daemonctl

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// FindPID returns the PID of the current user's rex-daemon. It is for
// display only (status lines and messages): never signal the result. Stop
// and Reload go through the socket, which names the daemon unambiguously.
//
// It matches the exact process name for the current UID only, so another
// user's daemon or a process whose command line merely contains
// "rex-daemon" is never reported. It returns ErrNotRunning when nothing
// matches and ErrMultipleDaemons when several daemons do.
func FindPID() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), pidLookupTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "pgrep", pgrepArgs()...).Output()
	return parsePgrep(out, err)
}

// pgrepArgs selects processes named exactly rex-daemon owned by this UID.
func pgrepArgs() []string {
	return []string{"-x", "-u", strconv.Itoa(os.Getuid()), "rex-daemon"}
}

// parsePgrep turns pgrep's output and error into FindPID's result.
func parsePgrep(out []byte, err error) (string, error) {
	if err != nil {
		// pgrep exits 1 when nothing matches.
		var ee *exec.ExitError
		if errors.As(err, &ee) && ee.ExitCode() == 1 {
			return "", ErrNotRunning
		}
		return "", fmt.Errorf("%w: pgrep: %w", ErrNotRunning, err)
	}
	pids := strings.Fields(string(out))
	switch len(pids) {
	case 0:
		return "", ErrNotRunning
	case 1:
		return pids[0], nil
	default:
		return "", fmt.Errorf("%w (pids %s)", ErrMultipleDaemons, strings.Join(pids, ", "))
	}
}
