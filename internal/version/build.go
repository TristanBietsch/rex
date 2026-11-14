package version

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"sync"
)

// Build is the first 12 hex digits of the SHA-256 of this executable, ""
// when it cannot be read. Two builds of the same Version differ here, so
// it tells a newly installed binary from the running one (R20).
var Build = sync.OnceValue(func() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return FileBuild(exe)
})

// FileBuild is Build of the binary at path.
func FileBuild(path string) string {
	f, err := os.Open(path) //nolint:gosec // G304: the caller's own executable.
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
}
