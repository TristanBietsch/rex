//go:build windows

package workspace

import "io/fs"

// skipModeChecks is true on Windows, where mode bits do not describe access.
const skipModeChecks = true

func ownedByCurrentUser(fs.FileInfo) bool { return true }

func isNotDirErr(error) bool { return false }
