package spawnctx

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"time"
)

// The workspace index (R16 limits, D9): the import picker and @ completion
// match against every file and folder of the session folder, listed once
// when the page opens. The walk stops at maxEntries or when its time budget
// is spent, whichever comes first, so a home-sized folder never hangs the
// page. In a git work tree it lists what `git ls-files -co
// --exclude-standard` lists (tracked and untracked, gitignored left out);
// elsewhere it walks the folder, skipping .git. It never enters a
// symlinked folder.

// Index is the walk's result: workspace-relative paths with "/"
// separators, folders ending in "/", sorted. Truncated is set when a cap
// stopped the walk; Links counts the symlinked folders seen and not
// entered.
type Index struct {
	Paths     []string
	Truncated bool
	Links     int
}

// now is the walk's clock; tests swap it.
var now = time.Now

// Walk lists root's files and folders, at most maxEntries of them, for at
// most budget. Cancelling ctx stops it with ctx's error.
func Walk(ctx context.Context, root string, maxEntries int, budget time.Duration) (Index, error) {
	return WalkCounted(ctx, root, maxEntries, budget, nil)
}

// WalkCounted is Walk that keeps n at the number of entries found so far,
// for a progress count read from another goroutine. n may be nil.
func WalkCounted(ctx context.Context, root string, maxEntries int, budget time.Duration, n *atomic.Int64) (Index, error) {
	w := &walker{ctx: ctx, max: maxEntries, deadline: now().Add(budget), n: n, seen: map[string]bool{}}
	// A root that is a symlink (a linked checkout, macOS /tmp) is walked
	// where it leads; the paths stay relative to it.
	if real, err := filepath.EvalSymlinks(root); err == nil {
		root = real
	}
	// The git commands get the budget on the real clock: a slow ls-files
	// is killed, and what it printed so far is kept.
	gctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	if inWorkTree(gctx, root) {
		err := w.git(gctx, root)
		if err == nil || ctx.Err() != nil {
			return w.index(), ctx.Err()
		}
		w.reset() // git failed: walk the folder instead
	}
	err := w.dir(root)
	return w.index(), err
}

type walker struct {
	ctx       context.Context
	max       int
	deadline  time.Time
	n         *atomic.Int64
	paths     []string
	seen      map[string]bool
	truncated bool
	links     int
}

func (w *walker) reset() {
	w.paths, w.seen, w.truncated, w.links = nil, map[string]bool{}, false, 0
	w.store()
}

func (w *walker) store() {
	if w.n != nil {
		w.n.Store(int64(len(w.paths)))
	}
}

func (w *walker) index() Index {
	slices.Sort(w.paths)
	return Index{Paths: w.paths, Truncated: w.truncated, Links: w.links}
}

// add records p once; false when a cap is reached (the walk stops).
func (w *walker) add(p string) bool {
	if w.seen[p] {
		return true
	}
	if len(w.paths) >= w.max || !now().Before(w.deadline) {
		w.truncated = true
		return false
	}
	w.seen[p] = true
	w.paths = append(w.paths, p)
	w.store()
	return true
}

// addFile records a file and, first, each folder above it.
func (w *walker) addFile(p string) bool {
	for i := range len(p) {
		if p[i] == '/' && !w.add(p[:i+1]) {
			return false
		}
	}
	return w.add(p)
}

// gitCommand is git with fixed arguments, run in dir without a shell. The
// environment drops every GIT_* variable (a GIT_DIR or GIT_WORK_TREE must
// not point the walk at another repo), takes no optional locks and never
// prompts; core.fsmonitor is off so the repo's config cannot run a hook.
func gitCommand(ctx context.Context, dir string, args ...string) *exec.Cmd {
	full := append([]string{"-c", "core.fsmonitor=false", "-c", "core.quotepath=off"}, args...)
	cmd := exec.CommandContext(ctx, "git", full...) //nolint:gosec // G204: fixed arguments, run directly, never through a shell.
	cmd.Dir = dir
	env := []string{"GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0"}
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(kv), "GIT_") {
			env = append(env, kv)
		}
	}
	cmd.Env = env
	cmd.WaitDelay = time.Second
	return cmd
}

// inWorkTree reports whether root is inside a git work tree (false without
// git).
func inWorkTree(ctx context.Context, root string) bool {
	out, err := gitCommand(ctx, root, "rev-parse", "--is-inside-work-tree").Output()
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

// git lists root through git ls-files, streaming its NUL-separated output.
// Stopping at a cap, or git killed at the budget, keeps what was listed.
func (w *walker) git(ctx context.Context, root string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := gitCommand(ctx, root, "ls-files", "-co", "--exclude-standard", "-z")
	pr, pw := io.Pipe()
	cmd.Stdout = pw
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = pw.CloseWithError(cmd.Wait()) }()
	defer func() { _ = pr.Close() }()
	sc := bufio.NewScanner(pr)
	sc.Split(splitNUL)
	for sc.Scan() {
		if !w.addFile(sc.Text()) {
			return nil // a cap: cancel kills git
		}
	}
	if ctx.Err() != nil {
		w.truncated = true // the budget killed git
		return nil
	}
	return sc.Err()
}

// splitNUL is a bufio.SplitFunc for NUL-terminated records.
func splitNUL(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if i := bytes.IndexByte(data, 0); i >= 0 {
		return i + 1, data[:i], nil
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}

// dir walks root without git: .git is skipped, a symlinked folder is
// counted and not entered, a link to a file is listed, anything else that
// is not a file or folder is left out, and an unreadable folder is listed
// but not entered.
func (w *walker) dir(root string) error {
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if cerr := w.ctx.Err(); cerr != nil {
			return cerr
		}
		if p == root {
			return err
		}
		if err != nil {
			return nil // unreadable: already listed, not entered
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if d.Name() == ".git" {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		ok := true
		switch {
		case d.IsDir():
			ok = w.add(rel + "/")
		case d.Type()&fs.ModeSymlink != 0:
			fi, serr := os.Stat(p)
			switch {
			case serr != nil:
			case fi.IsDir():
				w.links++
			case fi.Mode().IsRegular():
				ok = w.add(rel)
			}
		case d.Type().IsRegular():
			ok = w.add(rel)
		}
		if !ok {
			return filepath.SkipAll
		}
		return nil
	})
}

// Match is the paths of idx that hold query's letters in order, ignoring
// case, best first: the query whole in the base name, then whole in the
// path, then spread over the base name, then over the path; shorter paths
// first within each, then by name. An empty query is the index in order.
// limit caps the result (0 for all).
func Match(idx Index, query string, limit int) []string {
	q := strings.ToLower(query)
	type hit struct {
		p    string
		tier int
	}
	var hits []hit
	for _, p := range idx.Paths {
		lp := strings.ToLower(p)
		if !subsequence(q, lp) {
			continue
		}
		base := lp[strings.LastIndexByte(strings.TrimSuffix(lp, "/"), '/')+1:]
		tier := 3
		switch {
		case strings.Contains(base, q):
			tier = 0
		case strings.Contains(lp, q):
			tier = 1
		case subsequence(q, base):
			tier = 2
		}
		hits = append(hits, hit{p, tier})
	}
	if q != "" {
		slices.SortStableFunc(hits, func(a, b hit) int {
			if a.tier != b.tier {
				return a.tier - b.tier
			}
			if len(a.p) != len(b.p) {
				return len(a.p) - len(b.p)
			}
			return strings.Compare(a.p, b.p)
		})
	}
	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = h.p
	}
	return out
}

// subsequence reports whether q's bytes appear in s in order.
func subsequence(q, s string) bool {
	for i := 0; i < len(q); i++ {
		j := strings.IndexByte(s, q[i])
		if j < 0 {
			return false
		}
		s = s[j+1:]
	}
	return true
}

// dirKey is dir as an index folder: "" for the root, else ending in "/".
func dirKey(dir string) string {
	if dir == "" || strings.HasSuffix(dir, "/") {
		return dir
	}
	return dir + "/"
}

// Children is what idx lists directly in folder dir ("" for the root):
// folders first, then files, each sorted.
func Children(idx Index, dir string) []string {
	dir = dirKey(dir)
	var dirs, files []string
	for _, p := range idx.Paths {
		rest, ok := strings.CutPrefix(p, dir)
		if !ok || rest == "" {
			continue
		}
		switch i := strings.IndexByte(rest, '/'); {
		case i < 0:
			files = append(files, p)
		case i == len(rest)-1:
			dirs = append(dirs, p)
		}
	}
	return append(dirs, files...)
}

// DirFiles is how many files idx lists anywhere under folder dir.
func DirFiles(idx Index, dir string) int {
	dir = dirKey(dir)
	n := 0
	for _, p := range idx.Paths {
		if strings.HasPrefix(p, dir) && !strings.HasSuffix(p, "/") {
			n++
		}
	}
	return n
}
