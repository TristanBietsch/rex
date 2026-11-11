package snapshot

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// repoIgnores checks that cwd is inside a git worktree and returns exclude
// lines, anchored and relative to cwd, for everything the user's git ignores
// under cwd: the repo's .gitignore files (those above cwd too), its
// info/exclude, nested ones. The shadow store applies exactly these, so a
// snapshot of a repo subfolder skips node_modules and .env as git does. It
// only reads the user's repo.
func repoIgnores(ctx context.Context, cwd string) ([]string, error) {
	if err := CheckGit(ctx); err != nil {
		return nil, err
	}
	out, err := run(ctx, readOnlyEnv(), cwd, nil, "-c", "core.fsmonitor=false", "rev-parse", "--is-inside-work-tree")
	if err != nil {
		if ce := ctx.Err(); ce != nil {
			return nil, ce
		}
		if strings.Contains(err.Error(), "dubious ownership") {
			return nil, ErrDubiousOwnership
		}
		return nil, ErrNotGitWorktree
	}
	if strings.TrimSpace(string(out)) != "true" {
		return nil, ErrNotGitWorktree
	}
	out, err = run(ctx, readOnlyEnv(), cwd, nil, "-c", "core.fsmonitor=false",
		"ls-files", "-z", "-o", "-i", "--exclude-standard", "--directory")
	if err != nil {
		return nil, err
	}
	var lines []string
	for p := range strings.SplitSeq(string(out), "\x00") {
		if p == "" {
			continue
		}
		dir := strings.HasSuffix(p, "/")
		l := excludeLine(strings.TrimSuffix(p, "/"))
		if l == "" {
			continue
		}
		if dir {
			l += "/"
		}
		lines = append(lines, l)
	}
	return lines, nil
}

// excludeLine escapes a slash path into one anchored gitignore pattern.
func excludeLine(rel string) string {
	var b strings.Builder
	b.WriteByte('/')
	for i := 0; i < len(rel); i++ {
		switch c := rel[i]; c {
		case '\\', '*', '?', '[', ']':
			b.WriteByte('\\')
			b.WriteByte(c)
		case ' ':
			b.WriteString(`\ `)
		case '\n':
			// Cannot be written as a pattern; such a name is captured.
			return ""
		default:
			b.WriteByte(c)
		}
	}
	// The leading slash anchors the pattern, so '#' and '!' are literal.
	return b.String()
}

// Capture records the current state of cwd as a tree for session, stores a
// ref that keeps it alive, appends a ledger line and returns the tree id.
// It fails with ErrNotGitWorktree for a folder outside a git worktree. It
// never writes to the user's repo.
func (s *Store) Capture(ctx context.Context, cwd, session string, phase Phase) (Tree, error) {
	return s.CaptureWithin(ctx, cwd, session, phase, 0)
}

// CaptureWithin is Capture with a time limit that starts once the folder's
// lock is held: waiting behind an undo or another capture does not use it
// up. A limit of 0 means none beyond ctx.
func (s *Store) CaptureWithin(ctx context.Context, cwd, session string, phase Phase, limit time.Duration) (Tree, error) {
	if err := checkCWD(cwd); err != nil {
		return "", err
	}
	if err := checkSession(session); err != nil {
		return "", err
	}
	switch phase {
	case PhaseStart, PhaseEnd, PhaseSafety, PhasePreRedo:
	default:
		return "", fmt.Errorf("snapshot: bad capture phase %q", phase)
	}
	dir := s.dirFor(cwd)
	unlock, err := s.lockCtx(ctx, dir)
	if err != nil {
		return "", err
	}
	defer unlock()
	if limit > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, limit)
		defer cancel()
	}
	return s.capture(ctx, dir, cwd, session, phase)
}

// capture is Capture with the folder lock held.
func (s *Store) capture(ctx context.Context, dir, cwd, session string, phase Phase) (Tree, error) {
	tree, env, err := s.writeTree(ctx, dir, cwd, session)
	if err != nil {
		return "", err
	}
	n, err := s.nextSeq(ctx, env, cwd, session)
	if err != nil {
		return "", err
	}
	if _, err := run(ctx, env, cwd, nil, append(safeConfig(), "update-ref", refName(session, n), string(tree))...); err != nil {
		return "", err
	}
	entries, err := readLedger(dir)
	if err != nil {
		return "", err
	}
	p := fold(entries, session)
	e := Entry{SessionID: session, Phase: phase, Tree: tree, At: s.now().UTC(), Turn: p.cursor}
	if phase == PhaseStart {
		e.Turn = p.cursor + 1
	}
	if p.last != "" && p.last != tree {
		ch, err := s.changed(ctx, env, cwd, p.last, tree)
		if err != nil {
			return "", err
		}
		e.FilesChanged = len(ch)
	}
	if err := appendLedger(dir, e); err != nil {
		return "", err
	}
	return tree, nil
}

// writeTree stages the folder into the session's private index and writes
// the tree. Nothing keeps the tree alive yet: no ref, no ledger line. It
// also returns the environment for the session's shadow calls.
func (s *Store) writeTree(ctx context.Context, dir, cwd, session string) (Tree, []string, error) {
	exclude, err := repoIgnores(ctx, cwd)
	if err != nil {
		return "", nil, err
	}
	index := filepath.Join(shadowOf(dir), "index-"+session)
	env := gitEnv(shadowOf(dir), cwd, index)
	// An earlier capture killed at its time limit can leave its index lock
	// behind; this folder's lock means nobody else holds it.
	_ = os.Remove(index + ".lock")
	if err := s.ensure(ctx, dir, cwd, exclude, nil); err != nil {
		return "", nil, err
	}
	if err := s.dropDirConflicts(ctx, env, cwd); err != nil {
		return "", nil, err
	}
	big, err := s.bigUntracked(ctx, env, cwd)
	if err != nil {
		return "", nil, err
	}
	if len(big) > 0 {
		if err := s.ensure(ctx, dir, cwd, exclude, big); err != nil {
			return "", nil, err
		}
	}
	g := func(args ...string) ([]byte, error) {
		return run(ctx, env, cwd, nil, append(safeConfig(), args...)...)
	}
	if _, err := g("add", "-A", "--", "."); err != nil {
		return "", nil, err
	}
	out, err := g("write-tree")
	if err != nil {
		return "", nil, err
	}
	return Tree(strings.TrimSpace(string(out))), env, nil
}

func refName(session string, n int) string {
	return "refs/rex/" + session + "/" + strconv.Itoa(n)
}

// nextSeq is one more than the highest ref number the session has.
func (s *Store) nextSeq(ctx context.Context, env []string, cwd, session string) (int, error) {
	out, err := run(ctx, env, cwd, nil, append(safeConfig(), "for-each-ref", "--format=%(refname)", "refs/rex/"+session+"/")...)
	if err != nil {
		return 0, err
	}
	top := 0
	for l := range strings.SplitSeq(string(out), "\n") {
		i := strings.LastIndexByte(l, '/')
		if i < 0 {
			continue
		}
		if n, err := strconv.Atoi(l[i+1:]); err == nil && n > top {
			top = n
		}
	}
	return top + 1, nil
}

// dropDirConflicts removes from the session's index the files that are now
// directories on disk (a file replaced by a folder, a clone, a nested repo),
// so the folder is then judged as untracked content, not as an update of
// that file.
func (s *Store) dropDirConflicts(ctx context.Context, env []string, cwd string) error {
	out, err := run(ctx, env, cwd, nil, append(safeConfig(), "ls-files", "-z")...)
	if err != nil {
		return err
	}
	var gone []byte
	for p := range bytes.SplitSeq(out, []byte{0}) {
		if len(p) == 0 {
			continue
		}
		if fi, err := os.Lstat(filepath.Join(cwd, filepath.FromSlash(string(p)))); err == nil && fi.IsDir() {
			gone = append(gone, p...)
			gone = append(gone, 0)
		}
	}
	if len(gone) == 0 {
		return nil
	}
	_, err = run(ctx, env, cwd, gone, append(safeConfig(), "update-index", "--force-remove", "-z", "--stdin")...)
	return err
}

// bigUntracked lists the untracked, not-ignored files over MaxUntrackedBytes
// and the nested repos as exclude lines, so the next "add -A" skips them. A file the session's
// index already holds is not untracked and stays captured.
func (s *Store) bigUntracked(ctx context.Context, env []string, cwd string) ([]string, error) {
	out, err := run(ctx, env, cwd, nil, append(safeConfig(), "ls-files", "-o", "--exclude-standard", "-z")...)
	if err != nil {
		return nil, err
	}
	var big []string
	for _, p := range bytes.Split(out, []byte{0}) {
		rel := string(p)
		if rel == "" {
			continue
		}
		if strings.HasSuffix(rel, "/") {
			// A nested repo: skipped whole, never entered or recorded as a link.
			if l := excludeLine(strings.TrimSuffix(rel, "/")); l != "" {
				big = append(big, l+"/")
			}
			continue
		}
		fi, err := os.Lstat(filepath.Join(cwd, filepath.FromSlash(rel)))
		if err != nil || !fi.Mode().IsRegular() || fi.Size() <= MaxUntrackedBytes {
			continue
		}
		if l := excludeLine(rel); l != "" {
			big = append(big, l)
		}
	}
	return big, nil
}
