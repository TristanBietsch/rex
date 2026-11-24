//go:build linux

package server

import (
	"fmt"
	"os"
	"strconv"
)

// procExe returns the executable path of process pid from /proc/<pid>/exe,
// which the kernel maintains. The path keeps a " (deleted)" suffix when the
// binary was replaced or removed while the process ran.
func procExe(pid int) (string, error) {
	exe, err := os.Readlink("/proc/" + strconv.Itoa(pid) + "/exe")
	if err != nil {
		return "", fmt.Errorf("process %d: exe: %w", pid, err)
	}
	return exe, nil
}
