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
	stat, err := readStat(pid)
	if err != nil {
		return 0, err
	}
	return parseStatPPID(pid, stat)
}

// sessionOf returns the session id of process pid, read from
// /proc/<pid>/stat. It fails for a pid that does not exist.
func sessionOf(pid int) (int, error) {
	stat, err := readStat(pid)
	if err != nil {
		return 0, err
	}
	return parseStatSID(pid, stat)
}

func readStat(pid int) (string, error) {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return "", fmt.Errorf("process %d: %w", pid, err)
	}
	return string(b), nil
}

// parseStatPPID extracts the ppid field from a /proc/<pid>/stat line:
// "pid (comm) state ppid pgrp session ...".
func parseStatPPID(pid int, stat string) (int, error) { return statField(pid, stat, 1, "ppid") }

// parseStatSID extracts the session field from a /proc/<pid>/stat line.
func parseStatSID(pid int, stat string) (int, error) { return statField(pid, stat, 3, "session") }

// statField returns field i (0 is the state) of a /proc/<pid>/stat line as
// an int. comm may hold spaces and parentheses, so the fields are counted
// from the last ')'.
func statField(pid int, stat string, i int, name string) (int, error) {
	end := strings.LastIndexByte(stat, ')')
	if end < 0 {
		return 0, fmt.Errorf("process %d: malformed stat", pid)
	}
	fields := strings.Fields(stat[end+1:])
	if len(fields) <= i {
		return 0, fmt.Errorf("process %d: malformed stat", pid)
	}
	v, err := strconv.Atoi(fields[i])
	if err != nil {
		return 0, fmt.Errorf("process %d: malformed stat %s: %w", pid, name, err)
	}
	return v, nil
}
