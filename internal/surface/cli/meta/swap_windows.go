//go:build windows

package meta

import "errors"

// execSwap is not available on Windows (no execve): restart the board by
// hand to load a new build.
func execSwap(string) error {
	return errors.New("hot swap is not supported on Windows; quit and start rex again")
}
