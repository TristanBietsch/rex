//go:build !windows

package daemonctl

// daemonEnv is nil on Unix: the daemon starts with the caller's environment
// and filters its own by re-executing itself (boot.ReexecFiltered).
func daemonEnv() []string { return nil }
