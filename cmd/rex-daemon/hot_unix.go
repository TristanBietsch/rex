//go:build !windows

package main

import "syscall"

// killOrphan SIGKILLs the process group of a handed-over child nothing can
// supervise (its pgid is its pid: it was started with Setsid).
func killOrphan(pid int) {
	if pid > 0 {
		_ = syscall.Kill(-pid, syscall.SIGKILL)
	}
}
