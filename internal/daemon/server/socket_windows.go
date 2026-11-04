//go:build windows

package server

// restrictSocket is a no-op on Windows: mode bits only toggle the read-only
// attribute there. An AF_UNIX socket file takes the ACL of its directory,
// which lives in the user's profile.
func restrictSocket(string) error { return nil }
