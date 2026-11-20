//go:build linux

package server

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// parentPID returns the parent pid of process pid, read from
// /proc/<pid>/stat. It fails for a pid that does not exist.
func parentPID(pid int) (int, error) {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0, fmt.Errorf("process %d: %w", pid, err)
	}
	return parseStatPPID(pid, string(b))
}

// parseStatPPID extracts the ppid field from a /proc/<pid>/stat line:
// "pid (comm) state ppid ...". comm may hold spaces and parentheses, so the
// fields are counted from the last ')'.
func parseStatPPID(pid int, stat string) (int, error) {
	i := strings.LastIndexByte(stat, ')')
	if i < 0 {
		return 0, fmt.Errorf("process %d: malformed stat", pid)
	}
	fields := strings.Fields(stat[i+1:])
	if len(fields) < 2 {
		return 0, fmt.Errorf("process %d: malformed stat", pid)
	}
	ppid, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, fmt.Errorf("process %d: malformed stat ppid: %w", pid, err)
	}
	return ppid, nil
}
