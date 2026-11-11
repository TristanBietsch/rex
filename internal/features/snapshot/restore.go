package snapshot

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// FileChange is one path that differs between two trees. Status is git's
// letter: A added, M modified, D deleted, T type changed.
type FileChange struct {
	Path   string
	Status string
}

// RestoreOpts configures Restore.
type RestoreOpts struct {
	// Session names the private index Restore uses; required.
	Session string
	// From is the tree of the folder as it is now (a fresh capture). Files
	// in From but not in the target are deleted; required.
	From Tree
	// Guard, when set, runs after the plan and preflight and just before the
	// first file is touched; an error aborts with nothing changed.
	Guard Guard
}

// notStarted wraps an error returned before the restore changed anything: a
// refused plan or a tripped guard.
type notStarted struct{ err error }

func (e *notStarted) Error() string { return e.err.Error() }
func (e *notStarted) Unwrap() error { return e.err }

// Result lists what Restore did, as slash paths relative to the folder.
type Result struct {
	Restored []string
	Deleted  []string
}

const gitlinkMode = "160000"

// parseRaw parses "git diff-tree -r -z --raw" output into changes, leaving
// out submodule links.
func parseRaw(out []byte) []FileChange {
	var res []FileChange
	f := bytes.Split(out, []byte{0})
	for i := 0; i+1 < len(f); i += 2 {
		meta := strings.Fields(strings.TrimPrefix(string(f[i]), ":"))
		if len(meta) < 5 {
			continue
		}
		if meta[0] == gitlinkMode || meta[1] == gitlinkMode {
			continue
		}
		res = append(res, FileChange{Path: string(f[i+1]), Status: meta[4][:1]})
	}
	return res
}

func (s *Store) changed(ctx context.Context, env []string, cwd string, a, b Tree) ([]FileChange, error) {
	out, err := run(ctx, env, cwd, nil, append(safeConfig(), "diff-tree", "-r", "-z", "--no-renames", "--raw", string(a), string(b))...)
	if err != nil {
		return nil, err
	}
	return parseRaw(out), nil
}

// Changed lists the paths that differ between trees a and b of cwd's store,
// sorted by path.
func (s *Store) Changed(ctx context.Context, cwd string, a, b Tree) ([]FileChange, error) {
	if err := checkCWD(cwd); err != nil {
		return nil, err
	}
	dir := s.dirFor(cwd)
	unlock, err := s.lockCtx(ctx, dir)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if _, err := os.Stat(shadowOf(dir)); err != nil {
		return nil, fmt.Errorf("snapshot: no store for %s", cwd)
	}
	ch, err := s.changed(ctx, gitEnv(shadowOf(dir), cwd, filepath.Join(shadowOf(dir), "index-diff")), cwd, a, b)
	if err != nil {
		return nil, err
	}
	slices.SortFunc(ch, func(x, y FileChange) int { return strings.Compare(x.Path, y.Path) })
	return ch, nil
}

// Restore makes cwd match tree to: files the target has are written, files
// only opts.From has are deleted unless the folder's ignore rules cover
// them. Gitignored files that were never captured are never touched.
func (s *Store) Restore(ctx context.Context, cwd string, to Tree, opts RestoreOpts) (Result, error) {
	if err := checkCWD(cwd); err != nil {
		return Result{}, err
	}
	dir := s.dirFor(cwd)
	unlock, err := s.lockCtx(ctx, dir)
	if err != nil {
		return Result{}, err
	}
	defer unlock()
	return s.restore(ctx, dir, cwd, to, opts)
}

func (s *Store) restore(ctx context.Context, dir, cwd string, to Tree, opts RestoreOpts) (Result, error) {
	if err := checkSession(opts.Session); err != nil {
		return Result{}, err
	}
	if opts.From == "" || to == "" {
		return Result{}, errors.New("snapshot: restore needs both a target and a current tree")
	}
	shadow := shadowOf(dir)
	idx := filepath.Join(shadow, "index-restore-"+opts.Session)
	defer func() { _ = os.Remove(idx) }()
	env := gitEnv(shadow, cwd, idx)
	g := func(stdin []byte, args ...string) ([]byte, error) {
		return run(ctx, env, cwd, stdin, append(safeConfig(), args...)...)
	}
	write, del, err := s.plan(ctx, env, cwd, opts.From, to)
	if err != nil {
		return Result{}, &notStarted{err}
	}
	if s.afterPlan != nil {
		s.afterPlan()
	}
	if opts.Guard != nil {
		if gerr := opts.Guard(); gerr != nil {
			return Result{}, &notStarted{gerr}
		}
	}
	// Past this point the restore runs to the end: no deadline and no
	// cancellation of the caller's context stops it half way.
	ctx = context.WithoutCancel(ctx)
	if err := s.deleteAll(cwd, del); err != nil {
		return Result{}, err
	}
	if len(write) > 0 {
		if _, err := g(nil, "read-tree", string(to)); err != nil {
			return Result{}, err
		}
		in := []byte(strings.Join(write, "\x00") + "\x00")
		if _, err := g(in, "checkout-index", "-f", "-z", "--stdin"); err != nil {
			return Result{}, err
		}
	}
	return Result{Restored: write, Deleted: del}, nil
}

// plan lists what restoring to would write and delete from the state from.
func (s *Store) plan(ctx context.Context, env []string, cwd string, from, to Tree) (write, del []string, err error) {
	changes, err := s.changed(ctx, env, cwd, from, to)
	if err != nil {
		return nil, nil, err
	}
	for _, c := range changes {
		if !filepath.IsLocal(filepath.FromSlash(c.Path)) {
			return nil, nil, fmt.Errorf("snapshot: refusing path %q in a tree", c.Path)
		}
		if c.Status == "D" {
			del = append(del, c.Path)
		} else {
			write = append(write, c.Path)
		}
	}
	if del, err = s.dropIgnored(ctx, env, cwd, del); err != nil {
		return nil, nil, err
	}
	slices.Sort(write)
	slices.Sort(del)
	if err := preflight(ctx, env, cwd, from, write, del); err != nil {
		return nil, nil, err
	}
	return write, del, nil
}

// dropIgnored removes from paths those the folder's ignore rules cover:
// a gitignored file is never deleted.
func (s *Store) dropIgnored(ctx context.Context, env []string, cwd string, paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	in := []byte(strings.Join(paths, "\x00") + "\x00")
	cmd := exec.CommandContext(ctx, "git", append(safeConfig(), "check-ignore", "--no-index", "-z", "--stdin")...)
	cmd.Env, cmd.Dir, cmd.Stdin = env, cwd, bytes.NewReader(in)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		// Exit 1 means no path is ignored.
		if !errors.As(err, &ee) || ee.ExitCode() != 1 {
			return nil, fmt.Errorf("git check-ignore: %w: %s", err, strings.TrimSpace(errb.String()))
		}
	}
	ignored := map[string]bool{}
	for p := range bytes.SplitSeq(out.Bytes(), []byte{0}) {
		if len(p) > 0 {
			ignored[string(p)] = true
		}
	}
	keep := paths[:0:0]
	for _, p := range paths {
		if !ignored[p] {
			keep = append(keep, p)
		}
	}
	return keep, nil
}

// deleteAll removes paths (slash, relative to cwd) and the directories they
// leave empty, through a root handle on cwd: no path is resolved through a
// symlink, so a parent swapped for a link to somewhere else cannot send a
// delete outside the folder.
func (s *Store) deleteAll(cwd string, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	root, err := os.OpenRoot(cwd)
	if err != nil {
		return fmt.Errorf("snapshot: %w", err)
	}
	defer root.Close()
	if s.beforeDelete != nil {
		s.beforeDelete()
	}
	for _, p := range paths {
		rel := filepath.FromSlash(p)
		if err := root.Remove(rel); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("snapshot: delete %s: %w", p, err)
		}
		for dir := filepath.Dir(rel); dir != "." && dir != string(filepath.Separator); dir = filepath.Dir(dir) {
			if root.Remove(dir) != nil {
				break
			}
		}
	}
	return nil
}
