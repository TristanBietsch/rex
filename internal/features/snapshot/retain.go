package snapshot

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Retention defaults (R01-1-5).
const (
	DefaultMaxAge   = 7 * 24 * time.Hour
	DefaultMaxBytes = int64(2) << 30
	DefaultGCEvery  = 24 * time.Hour
)

// RetainOpts configures Retain. Zero values take the defaults.
type RetainOpts struct {
	// MaxAge is how long a removed session's captures are kept.
	MaxAge time.Duration
	// MaxBytes caps one folder store; oldest sessions are dropped first.
	MaxBytes int64
	// GCEvery is the least time between "git gc --prune=now" runs per folder.
	GCEvery time.Duration
	// Live reports whether a session is still on the board. A session that
	// is in a ledger, not live and not yet marked removed (a daemon crash, a
	// missed event) is marked removed now, which starts its clock. nil: no
	// session is live.
	Live func(session string) bool
}

// RetainReport says what Retain did.
type RetainReport struct {
	// Dropped are "folderkey/session" for every session whose refs went.
	Dropped []string
	// GCRuns is how many folder stores were garbage collected.
	GCRuns int
}

// Retain applies the retention rules to every folder store: refs of
// sessions removed more than MaxAge ago are deleted, a store over MaxBytes
// loses its oldest sessions, and each store is garbage collected at most
// once per GCEvery. A folder store left with no sessions is deleted. The
// first error ends the run; what was done stays done.
func (s *Store) Retain(ctx context.Context, o RetainOpts) (RetainReport, error) {
	if o.MaxAge <= 0 {
		o.MaxAge = DefaultMaxAge
	}
	if o.MaxBytes <= 0 {
		o.MaxBytes = DefaultMaxBytes
	}
	if o.GCEvery <= 0 {
		o.GCEvery = DefaultGCEvery
	}
	var rep RetainReport
	ents, err := os.ReadDir(s.root)
	if errors.Is(err, os.ErrNotExist) {
		return rep, nil
	}
	if err != nil {
		return rep, fmt.Errorf("snapshot: %w", err)
	}
	for _, de := range ents {
		if !de.IsDir() {
			continue
		}
		dir := filepath.Join(s.root, de.Name())
		if _, err := os.Stat(shadowOf(dir)); err != nil {
			continue // not a folder store
		}
		if err := ctx.Err(); err != nil {
			return rep, err
		}
		if err := s.retainDir(ctx, dir, de.Name(), o, &rep); err != nil {
			return rep, err
		}
	}
	return rep, nil
}

type sessionAge struct {
	id        string
	first     time.Time
	removedAt time.Time
}

func (s *Store) retainDir(ctx context.Context, dir, key string, o RetainOpts, rep *RetainReport) error {
	defer s.lock(dir)()
	entries, err := readLedger(dir)
	if err != nil {
		return err
	}
	now := s.now().UTC()
	byID := map[string]*sessionAge{}
	var order []string
	for _, e := range entries {
		a, ok := byID[e.SessionID]
		if !ok {
			a = &sessionAge{id: e.SessionID, first: e.At}
			byID[e.SessionID] = a
			order = append(order, e.SessionID)
		}
		if e.Phase == phaseRemoved && a.removedAt.IsZero() {
			a.removedAt = e.At
		}
	}
	for _, id := range order {
		a := byID[id]
		if a.removedAt.IsZero() && (o.Live == nil || !o.Live(id)) {
			a.removedAt = now
			entries = append(entries, Entry{SessionID: id, Phase: phaseRemoved, At: now})
		}
	}
	cwd, _ := os.ReadFile(filepath.Join(dir, "cwd"))
	env := gitEnv(shadowOf(dir), string(cwd), filepath.Join(shadowOf(dir), "index-retain"))
	drop := map[string]bool{}
	for _, id := range order {
		if a := byID[id]; !a.removedAt.IsZero() && now.Sub(a.removedAt) > o.MaxAge {
			drop[id] = true
		}
	}
	dropped := 0
	dropNow := func() error {
		if len(drop) == 0 {
			return nil
		}
		ids := make([]string, 0, len(drop))
		for id := range drop {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		for _, id := range ids {
			if err := deleteRefs(ctx, env, dir, id); err != nil {
				return err
			}
			rep.Dropped = append(rep.Dropped, key+"/"+id)
		}
		entries = slices.DeleteFunc(entries, func(e Entry) bool { return drop[e.SessionID] })
		order = slices.DeleteFunc(order, func(id string) bool { return drop[id] })
		dropped += len(ids)
		clear(drop)
		return rewriteLedger(dir, entries)
	}
	if err := dropNow(); err != nil {
		return err
	}
	if len(order) == 0 {
		return os.RemoveAll(dir)
	}
	if err := rewriteLedger(dir, entries); err != nil {
		return err
	}
	if dropped == 0 && !gcStale(dir, now, o.GCEvery) && dirSize(dir) <= o.MaxBytes {
		return nil
	}
	// Collect garbage; while the store is still over the cap, drop the oldest
	// session and collect again.
	for {
		if err := gc(ctx, env, dir); err != nil {
			return err
		}
		rep.GCRuns++
		if dirSize(dir) <= o.MaxBytes || len(order) == 0 {
			return nil
		}
		oldest := slices.MinFunc(order, func(x, y string) int { return byID[x].first.Compare(byID[y].first) })
		drop[oldest] = true
		if err := dropNow(); err != nil {
			return err
		}
		if len(order) == 0 {
			return os.RemoveAll(dir)
		}
	}
}

// deleteRefs deletes every refs/rex/<session>/ ref and the session's private
// indexes.
func deleteRefs(ctx context.Context, env []string, dir, session string) error {
	out, err := run(ctx, env, dir, nil, append(safeConfig(), "for-each-ref", "--format=%(refname)", "refs/rex/"+session+"/")...)
	if err != nil {
		return err
	}
	var in strings.Builder
	for ref := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		if ref != "" {
			in.WriteString("delete " + ref + "\n")
		}
	}
	if in.Len() > 0 {
		if _, err := run(ctx, env, dir, []byte(in.String()), append(safeConfig(), "update-ref", "--stdin")...); err != nil {
			return err
		}
	}
	for _, f := range []string{"index-" + session, "index-restore-" + session} {
		if err := os.Remove(filepath.Join(shadowOf(dir), f)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("snapshot: %w", err)
		}
	}
	return nil
}

func gcStale(dir string, now time.Time, every time.Duration) bool {
	fi, err := os.Stat(filepath.Join(dir, "last-gc"))
	return err != nil || now.Sub(fi.ModTime()) >= every
}

func gc(ctx context.Context, env []string, dir string) error {
	if _, err := run(ctx, env, dir, nil, append(safeConfig(), "gc", "--prune=now", "-q")...); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "last-gc"), nil, 0o600); err != nil {
		return fmt.Errorf("snapshot: %w", err)
	}
	return nil
}

// dirSize sums the sizes of the regular files under dir; unreadable parts
// count as zero.
func dirSize(dir string) int64 {
	var n int64
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil //nolint:nilerr // best effort
		}
		if fi, err := d.Info(); err == nil {
			n += fi.Size()
		}
		return nil
	})
	return n
}

// FolderUsage is one folder store's size, for rex doctor.
type FolderUsage struct {
	Key      string
	CWD      string
	Bytes    int64
	Sessions int
}

// Usage lists every folder store under the root, sorted by folder.
func (s *Store) Usage() ([]FolderUsage, error) {
	ents, err := os.ReadDir(s.root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("snapshot: %w", err)
	}
	var out []FolderUsage
	for _, de := range ents {
		dir := filepath.Join(s.root, de.Name())
		if !de.IsDir() {
			continue
		}
		if _, err := os.Stat(shadowOf(dir)); err != nil {
			continue
		}
		cwd, _ := os.ReadFile(filepath.Join(dir, "cwd"))
		entries, err := readLedger(dir)
		if err != nil {
			return nil, err
		}
		seen := map[string]bool{}
		for _, e := range entries {
			seen[e.SessionID] = true
		}
		out = append(out, FolderUsage{Key: de.Name(), CWD: string(cwd), Bytes: dirSize(dir), Sessions: len(seen)})
	}
	return out, nil
}

// UntrackedEnvFiles lists untracked, not-ignored ".env*" files in the git
// folder cwd: files a snapshot would copy into the store. It only reads
// the user's repo.
func UntrackedEnvFiles(ctx context.Context, cwd string) ([]string, error) {
	if _, err := GitPath(); err != nil {
		return nil, err
	}
	out, err := run(ctx, readOnlyEnv(), cwd, nil, "-c", "core.fsmonitor=false",
		"ls-files", "-o", "--exclude-standard", "-z", "--", ":(glob).env*", ":(glob)**/.env*")
	if err != nil {
		return nil, ErrNotGitWorktree
	}
	var res []string
	for p := range strings.SplitSeq(string(out), "\x00") {
		if p != "" {
			res = append(res, p)
		}
	}
	return res, nil
}
