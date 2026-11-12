package setup

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
)

// checkAutoTrustDirs reports the folders in auto_trust_dirs the daemon would
// ignore (the home folder, one holding it, a relative path, one with "..")
// and those that do not exist. It returns no row when the setting is empty,
// and an info row naming the trusted folders when every entry is usable.
func checkAutoTrustDirs() []checkResult {
	const name = "auto_trust_dirs"
	store, _, err := settings.LoadDefault()
	if err != nil {
		return nil // checkSettingsLoad already reports it
	}
	value := store.String("auto_trust_dirs")
	if strings.TrimSpace(value) == "" {
		return nil
	}
	entries, err := settings.SplitTrustDirs(value)
	if err != nil {
		return []checkResult{{name, checkWarn, "ignored: " + err.Error()}}
	}
	var rows []checkResult
	var good []string
	for _, e := range entries {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		dir, err := settings.TrustDir(e)
		if err != nil {
			rows = append(rows, checkResult{name, checkWarn, "ignored: " + err.Error() + " (rex trust -rm)"})
			continue
		}
		if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
			rows = append(rows, checkResult{name, checkWarn, fmt.Sprintf("%s does not exist (rex trust -rm %s)", dir, e)})
			continue
		}
		good = append(good, filepath.Clean(dir))
	}
	if len(rows) == 0 {
		return []checkResult{{name, checkInfo, strings.Join(good, ", ")}}
	}
	return rows
}
