//go:build !windows

package core

import "os"

// enableVT is a no-op: Unix terminals always interpret escape sequences.
func enableVT(*os.File) (restore func()) { return func() {} }
