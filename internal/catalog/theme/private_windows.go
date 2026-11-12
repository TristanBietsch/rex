//go:build windows

package theme

import "os"

// Windows file modes carry no group or other bits and ownership is an ACL
// matter, so there is nothing to check here; SECURITY.md documents ACLs as
// out of scope.
func checkPrivateFile(os.FileInfo) error { return nil }

func checkPrivateDir(os.FileInfo, string) error { return nil }
