//go:build windows

package pty

import (
	"errors"
	"os/exec"
)

// adoptConsole is not supported on Windows (no hot restart).
func adoptConsole(*Adopted, []string) (*exec.Cmd, console, error) {
	return nil, nil, errors.New("adopt: not supported on Windows")
}
