package snapshot

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// repoExclude checks that cwd is inside a git worktree and returns the
// content of that repo's info/exclude. It only reads the user's repo.
func repoExclude(ctx context.Context, cwd string) ([]byte, error) {
	if _, err := GitPath(); err != nil {
		return nil, err
	}
	out, err := run(ctx, readOnlyEnv(), cwd, nil,
		"-c", "core.fsmonitor=false",
		"rev-parse", "--is-inside-work-tree", "--git-path", "info/exclude")
	if err != nil {
		if ce := ctx.Err(); ce != nil {
			return nil, ce
		}
		return nil, ErrNotGitWorktree
	}
	lines := strings.Split(strings.TrimRight(string(out), "\r\n"), "\n")
	if len(lines) < 2 || strings.TrimSpace(lines[0]) != "true" {
		return nil, ErrNotGitWorktree
	}
	p := strings.TrimRight(lines[1], "\r")
	if !filepath.IsAbs(p) {
		p = filepath.Join(cwd, filepath.FromSlash(p))
	}
	b, err := os.ReadFile(p)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("snapshot: %w", err)
	}
	return b, nil
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
	defer s.lock(dir)()
	return s.capture(ctx, dir, cwd, session, phase)
}

// capture is Capture with the folder lock held.
func (s *Store) capture(ctx context.Context, dir, cwd, session string, phase Phase) (Tree, error) {
	exclude, err := repoExclude(ctx, cwd)
	if err != nil {
		return "", err
	}
	env := gitEnv(shadowOf(dir), cwd, filepath.Join(shadowOf(dir), "index-"+session))
	if err := s.ensure(ctx, dir, cwd, exclude, nil); err != nil {
		return "", err
	}
	big, err := s.bigUntracked(ctx, env, cwd)
	if err != nil {
		return "", err
	}
	if len(big) > 0 {
		if err := s.ensure(ctx, dir, cwd, exclude, big); err != nil {
			return "", err
		}
	}
	g := func(stdin []byte, args ...string) ([]byte, error) {
		return run(ctx, env, cwd, stdin, append(safeConfig(), args...)...)
	}
	if _, err := g(nil, "add", "-A", "--", "."); err != nil {
		return "", err
	}
	out, err := g(nil, "write-tree")
	if err != nil {
		return "", err
	}
	tree := Tree(strings.TrimSpace(string(out)))
	n, err := s.nextSeq(ctx, env, cwd, session)
	if err != nil {
		return "", err
	}
	if _, err := g(nil, "update-ref", refName(session, n), string(tree)); err != nil {
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
