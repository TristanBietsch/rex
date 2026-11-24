//go:build windows

package boot

// ReexecFiltered is a no-op on Windows, which has no exec-in-place: the
// daemon cannot replace its own process image. daemonctl.Start builds the
// filtered environment before it starts the daemon instead.
func ReexecFiltered(exec func(argv0 string, argv []string, env []string) error) error {
	return nil
}
