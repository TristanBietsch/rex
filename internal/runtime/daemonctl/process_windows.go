//go:build windows

package daemonctl

import (
	"encoding/csv"
	"os/exec"
	"strings"

	"github.com/tristanbietsch/rex/internal/wire/client"
)

// FindPID returns the PID of the first rex-daemon.exe in the task list.
func FindPID() (string, error) {
	out, err := exec.Command("tasklist", "/FI", "IMAGENAME eq rex-daemon.exe", "/FO", "CSV", "/NH").Output()
	if err != nil {
		return "", ErrNotRunning
	}
	return parseTasklistPID(string(out))
}

// parseTasklistPID reads the PID column of `tasklist /FO CSV /NH` output.
// With no match tasklist prints an INFO line instead of CSV rows.
func parseTasklistPID(out string) (string, error) {
	rows, err := csv.NewReader(strings.NewReader(out)).ReadAll()
	if err != nil {
		return "", ErrNotRunning
	}
	for _, row := range rows {
		if len(row) >= 2 && strings.EqualFold(row[0], "rex-daemon.exe") {
			return row[1], nil
		}
	}
	return "", ErrNotRunning
}

// Windows cannot deliver SIGHUP/SIGTERM to the daemon, so reload and stop go
// over the socket as Reload and Shutdown intents.

// Reload asks the running rex-daemon to re-read tools.yaml.
func Reload() error {
	return sendIntent(DefaultSocket(), (*client.Client).Reload)
}

// Stop asks the running rex-daemon to shut down and waits up to
// StartTimeout for its socket to go away, so a restart can start a new one.
func Stop() error {
	socket := DefaultSocket()
	if err := sendIntent(socket, (*client.Client).Shutdown); err != nil {
		return err
	}
	waitSocketGone(socket)
	return nil
}

// StopQuietly stops a running daemon; ignores errors.
func StopQuietly() { _ = Stop() }
