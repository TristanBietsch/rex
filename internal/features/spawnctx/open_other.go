//go:build !unix

package spawnctx

import "os"

// openNonBlocking opens path read-only. Outside Unix there are no FIFOs on
// a plain path to block on.
func openNonBlocking(path string) (*os.File, error) {
	return os.Open(path)
}
