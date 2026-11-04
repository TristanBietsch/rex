//go:build !windows

package daemonctl

import "syscall"

// exeSuffix is the executable file extension ("" on Unix).
const exeSuffix = ""

// detachAttr starts rex-daemon in a new session (setsid). It leaves the
// spawning terminal's process group and has no controlling terminal, so
// Ctrl-C there (SIGINT to the foreground group) and the terminal's hangup
// cannot reach it.
func detachAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
