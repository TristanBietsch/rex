//go:build darwin

package server

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// parentPID returns the parent pid of process pid from the kernel's process
// table (sysctl kern.proc.pid). It fails for a pid that does not exist.
func parentPID(pid int) (int, error) {
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return 0, fmt.Errorf("process %d: kern.proc.pid: %w", pid, err)
	}
	if int(kp.Proc.P_pid) != pid {
		return 0, fmt.Errorf("process %d: not found", pid)
	}
	return int(kp.Eproc.Ppid), nil
}

// sessionOf returns the session id of process pid (getsid). It fails for a
// pid that does not exist.
func sessionOf(pid int) (int, error) {
	sid, err := unix.Getsid(pid)
	if err != nil {
		return 0, fmt.Errorf("process %d: getsid: %w", pid, err)
	}
	return sid, nil
}
