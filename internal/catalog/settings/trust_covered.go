package settings

import (
	"path/filepath"
	"strings"
)

// CoveredBy reports whether cwd is one of the folders in the auto_trust_dirs
// value, or inside one, and which entry matched. Both sides are resolved
// through symlinks. Entries TrustDir refuses cover nothing, as in the daemon.
func CoveredBy(cwd, value string) (string, bool) {
	c, ok := CanonicalDir(cwd)
	if !ok {
		return "", false
	}
	for _, entry := range filepath.SplitList(value) {
		cd, err := TrustDir(entry)
		if err != nil {
			continue
		}
		if c == cd || strings.HasPrefix(c, cd+string(filepath.Separator)) {
			return strings.TrimSpace(entry), true
		}
	}
	return "", false
}
