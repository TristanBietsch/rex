//go:build unix

package spawnctx

import (
	"os"
	"syscall"
)

// openNonBlocking opens path read-only with O_NONBLOCK, so a FIFO that
// slipped past the checks can never hang the caller; a regular file reads
// as usual.
func openNonBlocking(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
}
