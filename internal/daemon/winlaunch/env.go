package winlaunch

import (
	"io"
	"os"
	"os/exec"
)

// SystemEnv reads the real system: exec.LookPath, the file system.
func SystemEnv() Env {
	return Env{
		LookPath: exec.LookPath,
		ReadFile: readCapped,
		IsFile: func(path string) bool {
			fi, err := os.Stat(path)
			return err == nil && fi.Mode().IsRegular()
		},
	}
}

// readCapped reads at most maxShimBytes+1 bytes of path, so an oversized
// file is seen as such without reading all of it.
func readCapped(path string) ([]byte, error) {
	f, err := os.Open(path) //nolint:gosec // G304: path is the resolved command, not user text.
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return io.ReadAll(io.LimitReader(f, maxShimBytes+1))
}
