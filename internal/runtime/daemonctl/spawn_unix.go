//go:build !windows

package daemonctl

import "syscall"

// exeSuffix is the executable file extension ("" on Unix).
const exeSuffix = ""

// detachAttr keeps the Unix spawn as before: rex-daemon shares rex's session.
func detachAttr() *syscall.SysProcAttr { return nil }
