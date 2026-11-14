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
	// Set in config.lua: rex trust -rm would refuse, so point at the line.
	fix := func(e string) string { return "rex trust -rm " + e }
	if at := store.SetAt("auto_trust_dirs"); at != "" {
		fix = func(string) string { return "set in " + at }
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
			rows = append(rows, checkResult{name, checkWarn, "ignored: " + err.Error() + " (" + fix(e) + ")"})
			continue
		}
		if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
			rows = append(rows, checkResult{name, checkWarn, fmt.Sprintf("%s does not exist (%s)", dir, fix(e))})
			continue
		}
		good = append(good, filepath.Clean(dir))
	}
	if len(rows) == 0 {
		detail := strings.Join(good, ", ")
		if at := store.SetAt("auto_trust_dirs"); at != "" {
			detail += " (set in " + at + ")"
		}
		return []checkResult{{name, checkInfo, detail}}
	}
	return rows
}
