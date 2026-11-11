package snapshot

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

// BlockedError means a restore would overwrite or remove something that no
// snapshot holds (a nested repo or clone, a file over 2 MiB, an ignored
// folder, a symlink) where it needs to write a file. Nothing was changed.
type BlockedError struct {
	// Paths are slash paths relative to the folder, sorted.
	Paths []string
}

func (e *BlockedError) Error() string {
	shown := e.Paths
	more := ""
	if len(shown) > 10 {
		more = fmt.Sprintf(" (and %d more)", len(shown)-10)
		shown = shown[:10]
	}
	return "nothing was changed: the restore would overwrite or remove content that no snapshot holds at " +
		strings.Join(shown, ", ") + more +
		". Move those paths out of the way (rename or move them elsewhere), then run it again"
}

// treeModes returns path -> git file mode for every file in tree.
func treeModes(ctx context.Context, env []string, cwd string, tree Tree) (map[string]string, error) {
	out, err := run(ctx, env, cwd, nil, append(safeConfig(), "ls-tree", "-r", "-z", string(tree))...)
	if err != nil {
		return nil, err
	}
	m := map[string]string{}
	for e := range bytes.SplitSeq(out, []byte{0}) {
		meta, path, ok := strings.Cut(string(e), "\t")
		if !ok {
			continue
		}
		if f := strings.Fields(meta); len(f) >= 1 {
			m[path] = f[0]
		}
	}
	return m, nil
}

// maxWalked bounds how much of a directory sitting at a write path the
// preflight inspects before it calls the path blocked.
const maxWalked = 5000

// preflight checks every path a restore would write or delete against what
// is on disk, before anything is touched. A write path may be absent, or
// hold the same kind of entry that the current state (from) captured there;
// each of its parents must be a directory or a captured file that is about
// to be deleted. Anything else is content no snapshot holds, and the whole
// restore is refused with a *BlockedError.
func preflight(ctx context.Context, env []string, cwd string, from Tree, write, del []string) error {
	modes, err := treeModes(ctx, env, cwd, from)
	if err != nil {
		return err
	}
	delSet := map[string]bool{}
	for _, p := range del {
		delSet[p] = true
	}
	blocked := map[string]bool{}
	// On a case-insensitive filesystem (macOS, Windows) a Foo -> foo rename
	// shows as one path deleted and another written, yet both names are the
	// same file: index the captured paths by lowercase to tell.
	var fold map[string]string
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		fold = make(map[string]string, len(modes))
		for p := range modes {
			fold[strings.ToLower(p)] = p
		}
	}
	abs := func(p string) string { return filepath.Join(cwd, filepath.FromSlash(p)) }

	for _, p := range write {
		// Parents, outermost first.
		parts := strings.Split(p, "/")
		for i := 1; i < len(parts); i++ {
			a := strings.Join(parts[:i], "/")
			fi, err := os.Lstat(abs(a))
			if errors.Is(err, fs.ErrNotExist) {
				break
			}
			if err != nil {
				blocked[a] = true
				break
			}
			if !fi.IsDir() && !delSet[a] {
				blocked[a] = true
			}
		}
		fi, err := os.Lstat(abs(p))
		switch {
		case errors.Is(err, fs.ErrNotExist):
		case err != nil:
			blocked[p] = true
		case fi.IsDir():
			if !dirOnlyHolds(abs(p), cwd, delSet) {
				blocked[p] = true
			}
		default:
			mode, ok := modes[p]
			if !ok && fold != nil {
				// The same file under another spelling, which is deleted first.
				if alt, has := fold[strings.ToLower(p)]; has && alt != p && delSet[alt] {
					if afi, aerr := os.Lstat(abs(alt)); aerr == nil && os.SameFile(fi, afi) {
						mode, ok = modes[alt], true
					}
				}
			}
			if !ok || (fi.Mode()&fs.ModeSymlink != 0) != (mode == "120000") {
				blocked[p] = true
			}
		}
	}
	for _, p := range del {
		if fi, err := os.Lstat(abs(p)); err == nil && fi.IsDir() {
			blocked[p] = true
		}
	}
	if len(blocked) == 0 {
		return nil
	}
	paths := make([]string, 0, len(blocked))
	for p := range blocked {
		paths = append(paths, p)
	}
	slices.Sort(paths)
	return &BlockedError{Paths: paths}
}

// dirOnlyHolds reports whether every non-directory under dir is in delSet:
// the directory will be empty once the deletions are done.
func dirOnlyHolds(dir, cwd string, delSet map[string]bool) bool {
	n := 0
	ok := true
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			ok = false
			return filepath.SkipAll
		}
		if n++; n > maxWalked {
			ok = false
			return filepath.SkipAll
		}
		if d.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(cwd, p)
		if rerr != nil || !delSet[filepath.ToSlash(rel)] {
			ok = false
			return filepath.SkipAll
		}
		return nil
	})
	return ok
}
