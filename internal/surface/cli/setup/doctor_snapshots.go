package setup

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/features/snapshot"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
)

// envScanTimeout bounds the untracked-.env scan of one folder.
const envScanTimeout = 5 * time.Second

// checkSnapshots reports the turn-snapshot state: whether the feature is on,
// whether git is found, each folder store's size and, per snapshotted
// folder, untracked .env* files (a snapshot copies them into the store). It
// creates nothing.
func checkSnapshots() []checkResult {
	const name = "turn snapshots"
	st, _, _ := settings.LoadDefault()
	on := settings.FeatureOn(st, "turn_snapshots")
	root, err := paths.SnapshotsDir()
	if err != nil {
		return []checkResult{{name, checkWarn, err.Error()}}
	}
	gitPath, _ := snapshot.GitPath()
	gitCtx, gitCancel := context.WithTimeout(context.Background(), envScanTimeout)
	gitErr := snapshot.CheckGit(gitCtx)
	gitCancel()
	usage, usageErr := snapshot.NewStore(root).Usage()

	var rows []checkResult
	switch {
	case !on:
		rows = append(rows, checkResult{name, checkInfo, "off (rex config set turn_snapshots true)"})
	case gitErr != nil:
		rows = append(rows, checkResult{name, checkWarn, "on, but no snapshot can be taken: " + gitErr.Error()})
	default:
		rows = append(rows, checkResult{name, checkPass, "on"})
	}
	switch {
	case gitErr == nil:
		rows = append(rows, checkResult{"git binary", checkInfo, gitPath})
	case on && gitPath == "":
		rows = append(rows, checkResult{"git binary", checkWarn, "not found on PATH"})
	case on:
		rows = append(rows, checkResult{"git binary", checkWarn, gitPath + ": " + gitErr.Error()})
	}
	if usageErr != nil {
		return append(rows, checkResult{"snapshot store", checkWarn, usageErr.Error()})
	}
	for _, u := range usage {
		folder := u.CWD
		if folder == "" {
			folder = u.Key
		}
		rows = append(rows, checkResult{"snapshot store: " + folder, checkInfo,
			fmt.Sprintf("%s, %d session(s)", humanBytes(u.Bytes), u.Sessions)})
		if len(u.OverCap) > 0 {
			rows = append(rows, checkResult{"snapshot size cap: " + folder, checkWarn,
				"the 2 GiB cap dropped the snapshots of live session(s) (" + strings.Join(u.OverCap, "; ") + "): undo is gone for them"})
		}
		if u.CWD == "" || gitErr != nil {
			continue
		}
		if _, err := os.Stat(u.CWD); err != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), envScanTimeout)
		files, err := snapshot.UntrackedEnvFiles(ctx, u.CWD)
		cancel()
		if err == nil && len(files) > 0 {
			rows = append(rows, checkResult{"snapshot secrets: " + folder, checkWarn,
				"untracked and not ignored, so copied into the store: " + strings.Join(files, ", ") +
					" (add them to .gitignore to keep them out)"})
		}
	}
	return rows
}

// humanBytes renders n with a binary unit.
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGT"[exp])
}
