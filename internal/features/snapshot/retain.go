package snapshot

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
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
	// DroppedLive are "folderkey/session" for live sessions whose refs went
	// because the size cap left nothing else to drop.
	DroppedLive []string
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
	var errs []error
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
		// A folder store that fails does not stop the others.
		if err := s.retainDir(ctx, dir, de.Name(), o, &rep); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", de.Name(), err))
		}
	}
	return rep, errors.Join(errs...)
}

type sessionAge struct {
	id        string
	first     time.Time
	removedAt time.Time
}

func (s *Store) retainDir(ctx context.Context, dir, key string, o RetainOpts, rep *RetainReport) error {
	unlock, err := s.lockCtx(ctx, dir)
	if err != nil {
		return err
	}
	defer unlock()
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
	// Refs whose session the ledger no longer knows (it was reset after
	// corruption) are orphans: nothing can undo to them, but they are kept for
	// the same 7 days as a reset ledger's file and are the first to go when
	// the store is over its size cap.
	refd, err := refSessions(ctx, env, dir)
	if err != nil {
		return err
	}
	orphanAt := now
	if t, ok := resetTime(dir); ok {
		orphanAt = t
	}
	for _, id := range refd {
		if _, ok := byID[id]; !ok {
			byID[id] = &sessionAge{id: id, removedAt: orphanAt}
			order = append(order, id)
		}
	}
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
		return s.removeStore(dir, now, o.MaxAge)
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
		victim, live := capVictim(order, byID)
		if victim == "" {
			slog.Warn("snapshots: store is over its size cap and only live sessions are left", "folder", key)
			return nil
		}
		if live {
			slog.Warn("snapshots: size cap reached; dropping a live session's snapshots", "folder", key, "session", victim)
			rep.DroppedLive = append(rep.DroppedLive, key+"/"+victim)
			if err := noteOverCap(dir, victim, now); err != nil {
				return err
			}
		}
		drop[victim] = true
		if err := dropNow(); err != nil {
			return err
		}
		if len(order) == 0 {
			return s.removeStore(dir, now, o.MaxAge)
		}
	}
}

// removeStore deletes a folder store that has no sessions left. A store whose
// ledger was set aside as corrupt is kept until maxAge after that, so the old
// ledger file can still be used to recover by hand.
func (s *Store) removeStore(dir string, now time.Time, maxAge time.Duration) error {
	if t, ok := resetTime(dir); ok && now.Sub(t) <= maxAge {
		return nil
	}
	return os.RemoveAll(dir)
}

// refSessions lists the session ids that have refs under refs/rex/.
func refSessions(ctx context.Context, env []string, dir string) ([]string, error) {
	out, err := run(ctx, env, dir, nil, append(safeConfig(), "for-each-ref", "--format=%(refname)", "refs/rex/")...)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var ids []string
	for ref := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		parts := strings.Split(ref, "/") // refs/rex/<session>/<n>
		if len(parts) >= 4 && !seen[parts[2]] {
			seen[parts[2]] = true
			ids = append(ids, parts[2])
		}
	}
	return ids, nil
}

// capVictim picks the session to drop when a store is over its cap: the
// oldest session that has left the board, else (live true) the oldest live
// one, but never the newest live one, so a store with a live session is
// never emptied. "" means nothing may be dropped.
func capVictim(order []string, byID map[string]*sessionAge) (id string, live bool) {
	var gone, alive []string
	for _, s := range order {
		if byID[s].removedAt.IsZero() {
			alive = append(alive, s)
		} else {
			gone = append(gone, s)
		}
	}
	byFirst := func(x, y string) int { return byID[x].first.Compare(byID[y].first) }
	if len(gone) > 0 {
		return slices.MinFunc(gone, byFirst), false
	}
	if len(alive) > 1 {
		slices.SortFunc(alive, byFirst)
		return alive[0], true
	}
	return "", false
}

// noteOverCap records that the cap forced a live session's snapshots out,
// for rex doctor.
func noteOverCap(dir, session string, at time.Time) error {
	f, err := os.OpenFile(filepath.Join(dir, "over-cap"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("snapshot: %w", err)
	}
	_, werr := fmt.Fprintf(f, "%s %s\n", at.Format(time.RFC3339), session)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	return werr
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
	// OverCap lists live sessions whose snapshots the size cap dropped, as
	// "<time> <session>" lines.
	OverCap []string
	// Corrupt is set when the folder's ledger is damaged and not yet set
	// aside (the daemon does that on its next write). ResetAt and ResetFile
	// say the ledger was already set aside: older turns cannot be undone.
	Corrupt   bool
	ResetAt   time.Time
	ResetFile string
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
		unlock := s.lock(dir)
		entries, err := peekLedger(dir)
		unlock()
		corrupt := errors.Is(err, ErrLedgerCorrupt)
		if err != nil && !corrupt {
			return nil, err
		}
		var resetAt time.Time
		var resetFile string
		if re := readReset(dir); re != nil {
			resetAt, resetFile = re.At, re.File
		}
		var over []string
		if b, err := os.ReadFile(filepath.Join(dir, "over-cap")); err == nil {
			for l := range strings.SplitSeq(strings.TrimSpace(string(b)), "\n") {
				if l != "" {
					over = append(over, l)
				}
			}
		}
		seen := map[string]bool{}
		for _, e := range entries {
			seen[e.SessionID] = true
		}
		out = append(out, FolderUsage{Key: de.Name(), CWD: string(cwd), Bytes: dirSize(dir), Sessions: len(seen), OverCap: over,
			Corrupt: corrupt, ResetAt: resetAt, ResetFile: resetFile})
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
