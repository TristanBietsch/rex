//go:build !linux && !darwin

package server

// procExe is not implemented on this platform (see parentPID).
func procExe(int) (string, error) {
	return "", errProcTableUnsupported
}
