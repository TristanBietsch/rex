//go:build windows

package daemonctl

import (
	"context"
	"encoding/csv"
	"fmt"
	"os/exec"
	"os/user"
	"strings"
)

// FindPID returns the PID of the current user's rex-daemon.exe. It is for
// display only (status lines and messages): never act on the result. Stop
// and Reload go through the socket, which names the daemon unambiguously.
//
// It returns ErrNotRunning when nothing matches and ErrMultipleDaemons when
// several daemons do.
func FindPID() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), pidLookupTimeout)
	defer cancel()
	args := []string{"/FI", "IMAGENAME eq rex-daemon.exe", "/FO", "CSV", "/NH"}
	if u, err := user.Current(); err == nil {
		args = append(args, "/FI", "USERNAME eq "+u.Username)
	}
	out, err := exec.CommandContext(ctx, "tasklist", args...).Output()
	if err != nil {
		return "", fmt.Errorf("%w: tasklist: %w", ErrNotRunning, err)
	}
	return parseTasklistPID(string(out))
}

// parseTasklistPID reads the PID column of `tasklist /FO CSV /NH` output.
// With no match tasklist prints an INFO line instead of CSV rows.
func parseTasklistPID(out string) (string, error) {
	r := csv.NewReader(strings.NewReader(out))
	r.FieldsPerRecord = -1
	rows, err := r.ReadAll()
	if err != nil {
		return "", ErrNotRunning
	}
	var pids []string
	for _, row := range rows {
		if len(row) >= 2 && strings.EqualFold(row[0], "rex-daemon.exe") {
			pids = append(pids, row[1])
		}
	}
	switch len(pids) {
	case 0:
		return "", ErrNotRunning
	case 1:
		return pids[0], nil
	default:
		return "", fmt.Errorf("%w (pids %s)", ErrMultipleDaemons, strings.Join(pids, ", "))
	}
}
