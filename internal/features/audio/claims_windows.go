//go:build windows

package audio

import "io/fs"

// oNoFollow is 0: Windows has no O_NOFOLLOW. O_EXCL still refuses an
// existing name, symlink or not, and Claim checks the file with Lstat.
const oNoFollow = 0

// checkClaimDir has nothing to check: the folder sits in the user's
// profile, whose ACL keeps other users out, and Unix mode bits mean nothing.
func checkClaimDir(fs.FileInfo) error { return nil }
