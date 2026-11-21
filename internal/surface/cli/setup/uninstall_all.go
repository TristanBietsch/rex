package setup

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/workspace"
)

// rexLogNames are the names rexlog.Init is called with. A log file is
// <name>.log, so only these exact file names are removed from REX_LOG_DIR,
// which the user may share with other programs.
var rexLogNames = []string{"daemon", "tui", "onboard", "setup"}

// allTargets is what `-all` removes beyond the individual flags, and what it
// leaves behind. Everything is resolved before the first removal, because
// the workspace registry lives in the data directory that is removed.
type allTargets struct {
	dirs    []removal // whole directories: data and cache (Kind set, Path is the folder)
	files   []string  // single files: workspace indexes and audit logs, rex log files
	allowed []string  // roots every removal must sit under: home, each workspace's .rex/
	keep    []string  // paths deliberately left in place
	skipped []removal
}

// planAll resolves the extra -all removals. It touches nothing.
func planAll(home string) allTargets {
	var t allTargets
	l := paths.ForHome(home)
	t.allowed = []string{home}
	t.dirs = []removal{{Kind: "data", Path: l.DataDir}, {Kind: "cache", Path: l.CacheDir}}

	t.planWorkspaces(l.WorkspacesFile)
	t.planLogs(home)
	for _, prof := range profileFiles(home) {
		if m, err := filepath.Glob(prof + ".rex-backup-*"); err == nil {
			t.keep = append(t.keep, m...)
		}
	}
	return t
}

// planWorkspaces adds, for each workspace in the registry, its rex-written
// .rex/sessions.jsonl and .rex/audit.jsonl. config.lua and workspace.json
// are kept: the user may have committed them.
func (t *allTargets) planWorkspaces(registryPath string) {
	reg, err := workspace.OpenRegistry(registryPath)
	if err != nil {
		t.skip(registryPath, err.Error())
		return
	}
	entries, err := reg.List()
	if err != nil {
		t.skip(registryPath, "cannot read workspace list: "+err.Error())
		return
	}
	for _, e := range entries {
		root := filepath.Clean(e.Root)
		if !filepath.IsAbs(root) {
			t.skip(e.Root, "workspace root is not an absolute path")
			continue
		}
		if fi, err := os.Stat(root); err != nil || !fi.IsDir() {
			t.skip(root, "workspace folder no longer exists")
			continue
		}
		rexDir := filepath.Join(root, workspace.DirName)
		fi, err := os.Lstat(rexDir)
		switch {
		case os.IsNotExist(err):
			continue
		case err != nil:
			t.skip(rexDir, err.Error())
			continue
		case !fi.IsDir():
			t.skip(rexDir, "not a plain directory (symlink or file): not followed")
			continue
		}
		t.allowed = append(t.allowed, rexDir)
		for _, name := range []string{workspace.IndexFileName, workspace.AuditFileName} {
			t.addFile(filepath.Join(rexDir, name))
		}
		for _, name := range []string{workspace.ConfigFileName, workspace.FileName} {
			p := filepath.Join(rexDir, name)
			if _, err := os.Lstat(p); err == nil {
				t.keep = append(t.keep, p)
			}
		}
	}
}

// planLogs adds rex's log files under REX_LOG_DIR. The default log folder
// is removed whole by -wipe-state.
func (t *allTargets) planLogs(home string) {
	dir := os.Getenv("REX_LOG_DIR")
	if dir == "" {
		return
	}
	dir = filepath.Clean(dir)
	if !filepath.IsAbs(dir) || !isUnder(dir, home) {
		t.skip(dir, "REX_LOG_DIR is outside your home: log files left alone")
		return
	}
	for _, name := range rexLogNames {
		t.addFile(filepath.Join(dir, name+".log"))
	}
}

// addFile plans path for removal when it is a regular file. A symlink is
// reported and left alone; a missing file is ignored.
func (t *allTargets) addFile(path string) {
	fi, err := os.Lstat(path) //nolint:gosec // G703: path is built from the registry root and fixed names; Lstat only inspects it.
	switch {
	case os.IsNotExist(err):
	case err != nil:
		t.skip(path, err.Error())
	case !fi.Mode().IsRegular():
		t.skip(path, "not a regular file (symlink?): not followed")
	default:
		t.files = append(t.files, path)
	}
}

func (t *allTargets) skip(path, why string) {
	t.skipped = append(t.skipped, removal{Kind: "skipped", Path: path, Detail: why})
}

// permitted reports whether path sits under home or a listed workspace's
// .rex/ directory.
func (t *allTargets) permitted(path string) bool {
	for _, a := range t.allowed {
		if isUnder(path, a) {
			return true
		}
	}
	return false
}

// rows lists the plan: what goes, what stays, what was skipped.
func (t *allTargets) rows() []removal {
	var out []removal
	for _, d := range t.dirs {
		out = append(out, removal{Kind: d.Kind, Path: d.Path + "/", Detail: dirSummary(d.Path)})
	}
	for _, f := range t.files {
		out = append(out, removal{Kind: "file", Path: f})
	}
	return append(out, t.leftRows()...)
}

// leftRows lists what -all leaves in place.
func (t *allTargets) leftRows() []removal {
	var out []removal
	for _, k := range t.keep {
		out = append(out, removal{Kind: "kept", Path: k})
	}
	return append(out, t.skipped...)
}

// removeAll removes the planned directories and files, checking each again
// with Lstat right before it goes.
func (t *allTargets) removeAll(p interface{ Warn(string) }, done []removal, failed int) ([]removal, int) {
	for _, d := range t.dirs {
		if err := t.removeOne(d.Path, true); err != nil {
			p.Warn(fmt.Sprintf("error removing %s: %v", d.Path, err))
			failed++
			continue
		}
		done = append(done, d)
	}
	for _, f := range t.files {
		if err := t.removeOne(f, false); err != nil {
			p.Warn(fmt.Sprintf("error removing %s: %v", f, err))
			failed++
			continue
		}
		done = append(done, removal{Kind: "file", Path: f})
	}
	return done, failed
}

// removeOne removes path if it is still the plain directory or file it was
// planned as and sits inside an allowed root. Symlinks are never followed.
func (t *allTargets) removeOne(path string, dir bool) error {
	if !t.permitted(path) {
		return fmt.Errorf("refusing: outside your home and the listed workspaces")
	}
	fi, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if dir && !fi.IsDir() || !dir && !fi.Mode().IsRegular() {
		return fmt.Errorf("refusing: not a plain %s (symlink?)", map[bool]string{true: "directory", false: "file"}[dir])
	}
	if dir {
		return os.RemoveAll(path)
	}
	return os.Remove(path)
}
