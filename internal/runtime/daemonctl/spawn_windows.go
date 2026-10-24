//go:build windows

package daemonctl

import (
	"syscall"

	"golang.org/x/sys/windows"
)

// exeSuffix is the executable file extension.
const exeSuffix = ".exe"

// detachAttr gives rex-daemon its own hidden console and process group, so
// closing the terminal that started it (or Ctrl-C there) does not kill it.
func detachAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.CREATE_NO_WINDOW,
		HideWindow:    true,
	}
}
