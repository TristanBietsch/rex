package setup

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/workspace"
)

// rexLogBaseNames are the names rex passes to rexlog.Init or rexlog.Path
// (cmd/rex-daemon/main.go, setup/setup.go, setup/onboard.go, meta/tui.go,
// tui/splash_steps.go) apart from the CLI error log, whose name comes from
// core.ErrLogPath. A log file is <name>.log.
var rexLogBaseNames = []string{"daemon", "tui", "onboard", "setup"}

// rexLogFileNames is every file name rex writes in a log directory: the
// <name>.log files, the CLI error log and its ".1" rotation.
func rexLogFileNames() []string {
	var out []string
	for _, n := range rexLogBaseNames {
		out = append(out, n+".log")
	}
	return out
}

// target is one planned removal. guard is the directory whose identity is
// re-checked right before removal (the folder holding a file, or the parent
// of a planned directory); resolved is its symlink-free path at plan time.
type target struct {
	removal
	guard    string
	resolved string
	isDir    bool
}

// allTargets is what `-all` removes beyond the individual flags, and what it
// leaves behind. Everything is resolved before the first removal, because
// the workspace registry lives in the data directory that is removed.
type allTargets struct {
	dirs    []target
	files   []target
	keep    []removal // paths deliberately left in place
	skipped []removal
}

// planAll resolves the extra -all removals. It touches nothing.
func planAll(home string) allTargets {
	var t allTargets
	l := paths.ForHome(home)
	rhome, err := filepath.EvalSymlinks(home)
	if err != nil {
		rhome = home
	}
	t.addDir("data", l.DataDir, home, rhome)
	t.addDir("cache", l.CacheDir, home, rhome)
	t.planWorkspaces(l.WorkspacesFile)
	t.planLogs(home, rhome)
	for _, prof := range profileFiles(home) {
		if m, err := filepath.Glob(prof + ".rex-backup-*"); err == nil {
			for _, k := range m {
				t.keep = append(t.keep, removal{Kind: "kept", Path: k})
			}
		}
	}
	return t
}

// sameResolved resolves dir and requires it to sit at the same place with
// no symlink in any component, and inside within. The lexical form is
// compared after mapping home to its resolved path. It returns the resolved
// path; os.ErrNotExist when dir is missing.
func sameResolved(dir, within, home, rhome string) (string, error) {
	r, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", err
	}
	want := dir
	if isUnder(dir, home) {
		rel, _ := filepath.Rel(home, dir)
		want = filepath.Join(rhome, rel)
	}
	if r != want {
		return "", fmt.Errorf("path goes through a symlink (resolves to %s): not followed", r)
	}
	if !isUnder(r, within) {
		return "", errors.New("resolves outside the allowed folder: not followed")
	}
	return r, nil
}

// addDir plans a whole directory under home. A symlink or a path through
// one is skipped and reported; a missing directory is ignored.
func (t *allTargets) addDir(kind, dir, home, rhome string) {
	parent := filepath.Dir(dir)
	rp, err := sameResolved(parent, rhome, home, rhome)
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	if err != nil {
		t.skip(dir, err.Error())
		return
	}
	fi, err := os.Lstat(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return
	case err != nil:
		t.skip(dir, err.Error())
		return
	case fi.Mode().Type() != fs.ModeDir:
		t.skip(dir, "not a plain directory (symlink?): not followed")
		return
	}
	t.dirs = append(t.dirs, target{
		removal: removal{Kind: kind, Path: dir + "/", Detail: dirSummary(dir)},
		guard:   parent, resolved: rp, isDir: true,
	})
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
		t.planWorkspace(e.Root)
	}
}

func (t *allTargets) planWorkspace(rawRoot string) {
	root := filepath.Clean(rawRoot)
	if !filepath.IsAbs(root) {
		t.skip(rawRoot, "workspace root is not an absolute path")
		return
	}
	rr, err := filepath.EvalSymlinks(root)
	if errors.Is(err, fs.ErrNotExist) {
		t.skip(root, "workspace folder no longer exists")
		return
	}
	if err != nil {
		t.skip(root, err.Error())
		return
	}
	if rr != root {
		t.skip(root, "workspace root goes through a symlink (resolves to "+rr+"): not followed")
		return
	}
	rexDir := filepath.Join(root, workspace.DirName)
	fi, err := os.Lstat(rexDir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return
	case err != nil:
		t.skip(rexDir, err.Error())
		return
	case fi.Mode().Type() != fs.ModeDir:
		t.skip(rexDir, "not a plain directory (symlink or file): not followed")
		return
	}
	rexRes, err := sameResolved(rexDir, rexDir, rr, rr)
	if err != nil {
		t.skip(rexDir, err.Error())
		return
	}
	for _, name := range []string{workspace.IndexFileName, workspace.AuditFileName} {
		t.addFile(filepath.Join(rexDir, name), rexDir, rexRes)
	}
	for _, name := range []string{workspace.ConfigFileName, workspace.FileName} {
		p := filepath.Join(rexDir, name)
		if _, err := os.Lstat(p); err == nil {
			t.keep = append(t.keep, removal{Kind: "kept", Path: p})
		}
	}
}

// planLogs adds rex's log files under REX_LOG_DIR. The default log folder
// is removed whole by -wipe-state.
func (t *allTargets) planLogs(home, rhome string) {
	dir := os.Getenv("REX_LOG_DIR")
	if dir == "" {
		return
	}
	dir = filepath.Clean(dir)
	if !filepath.IsAbs(dir) || !isUnder(dir, home) {
		t.skip(dir, "REX_LOG_DIR is outside your home: log files left alone")
		return
	}
	res, err := sameResolved(dir, rhome, home, rhome)
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	if err != nil {
		t.skip(dir, "REX_LOG_DIR "+err.Error())
		return
	}
	for _, name := range rexLogFileNames() {
		t.addFile(filepath.Join(dir, name), dir, res)
	}
}

// addFile plans path for removal when it is a regular file inside guard
// (a plain directory that resolved to resolved). A symlink is reported and
// left alone; a missing file is ignored.
func (t *allTargets) addFile(path, guard, resolved string) {
	fi, err := os.Lstat(path) //nolint:gosec // G703: path is built from a checked folder and fixed names; Lstat only inspects it.
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		t.skip(path, err.Error())
	case !fi.Mode().IsRegular():
		t.skip(path, "not a regular file (symlink?): not followed")
	default:
		t.files = append(t.files, target{removal: removal{Kind: "file", Path: path}, guard: guard, resolved: resolved})
	}
}

func (t *allTargets) skip(path, why string) {
	t.skipped = append(t.skipped, removal{Kind: "skipped", Path: path, Detail: why})
}

// rows lists what -all removes.
func (t *allTargets) rows() []removal {
	var out []removal
	for _, d := range t.dirs {
		out = append(out, d.removal)
	}
	for _, f := range t.files {
		out = append(out, f.removal)
	}
	return out
}

// removeAll removes the planned directories and files. Before each, the
// guard folder is checked again: a plain directory that still resolves to
// what it did at plan time. Anything else is skipped with a warning.
func (t *allTargets) removeAll(p interface{ Warn(string) }, done []removal, failed int) ([]removal, int) {
	for _, tg := range append(append([]target{}, t.dirs...), t.files...) {
		path := tg.Path
		if tg.isDir {
			path = filepath.Clean(path)
		}
		switch err := tg.remove(path); {
		case err == nil:
			done = append(done, removal{Kind: tg.Kind, Path: path})
		case errors.Is(err, errChanged):
			p.Warn(fmt.Sprintf("skipped %s: %v", path, err))
		default:
			p.Warn(fmt.Sprintf("error removing %s: %v", path, err))
			failed++
		}
	}
	return done, failed
}

var errChanged = errors.New("changed since the plan (symlink?): not followed")

// remove re-checks the guard folder, then removes path if it is still the
// plain directory or file it was planned as. Symlinks are never followed.
func (tg target) remove(path string) error {
	gi, err := os.Lstat(tg.guard)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if gi.Mode().Type() != fs.ModeDir {
		return errChanged
	}
	if r, err := filepath.EvalSymlinks(tg.guard); err != nil || r != tg.resolved {
		return errChanged
	}
	fi, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if tg.isDir {
		if fi.Mode().Type() != fs.ModeDir {
			return errChanged
		}
		return os.RemoveAll(path)
	}
	if !fi.Mode().IsRegular() {
		return errChanged
	}
	return os.Remove(path)
}
