//go:build windows

package loop

import "io/fs"

// checkOwnerAndMode is a no-op on Windows: file modes carry no owner or
// group bits there, and the profile folder's ACL is the protection.
func checkOwnerAndMode(fs.FileInfo) error { return nil }
