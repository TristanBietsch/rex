package snapshot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Phase says when in a turn a capture was taken.
type Phase string

// Capture phases. Start and End bracket an agent turn. Safety is the state
// just before an undo (what redo restores); PreRedo is the state just before
// a redo and is only kept so its objects survive.
const (
	PhaseStart   Phase = "start"
	PhaseEnd     Phase = "end"
	PhaseSafety  Phase = "safety"
	PhasePreRedo Phase = "pre-redo"
)

// Ledger-only phases: operations, not captures.
const (
	phaseUndo    Phase = "undo"
	phaseRedo    Phase = "redo"
	phaseRemoved Phase = "removed"
)

// Tree is a git tree id: one captured state of a folder.
type Tree string

// MaxUntrackedBytes is the size above which an untracked file is skipped.
const MaxUntrackedBytes = 2 << 20

// Store is the root of all shadow stores, one folder store per working
// folder. Methods on one folder serialize; different folders run in
// parallel. A Store is safe for concurrent use.
type Store struct {
	root string
	now  func() time.Time

	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

// NewStore returns a Store rooted at root (~/.local/share/rex/snapshots).
// Nothing is created until the first capture.
func NewStore(root string) *Store {
	return &Store{root: root, now: time.Now, locks: map[string]*sync.Mutex{}}
}

// Root is the directory holding every folder store.
func (s *Store) Root() string { return s.root }

var sessionRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)

func checkSession(id string) error {
	if !sessionRe.MatchString(id) {
		return fmt.Errorf("snapshot: invalid session id %q", id)
	}
	return nil
}

func checkCWD(cwd string) error {
	if !filepath.IsAbs(cwd) {
		return fmt.Errorf("snapshot: folder %q is not an absolute path", cwd)
	}
	return nil
}

// folderKey is sha256(abs cwd)[:16] in hex. Windows paths compare case
// insensitively, so they are lowered first.
func folderKey(cwd string) string {
	p := filepath.Clean(cwd)
	if runtime.GOOS == "windows" {
		p = strings.ToLower(p)
	}
	sum := sha256.Sum256([]byte(p))
	return hex.EncodeToString(sum[:])[:16]
}

// dirFor is the folder store directory for cwd.
func (s *Store) dirFor(cwd string) string { return filepath.Join(s.root, folderKey(cwd)) }

func shadowOf(dir string) string { return filepath.Join(dir, "git") }
func ledgerOf(dir string) string { return filepath.Join(dir, "turns.jsonl") }

// lock serializes work on one folder store directory and returns the unlock.
func (s *Store) lock(dir string) func() {
	s.mu.Lock()
	l, ok := s.locks[dir]
	if !ok {
		l = &sync.Mutex{}
		s.locks[dir] = l
	}
	s.mu.Unlock()
	l.Lock()
	return l.Unlock
}

// ensure creates the folder store and its shadow git dir (0700) when
// missing and refreshes its info files. exclude is the user's repo exclude
// file content; extra are additional exclude lines.
func (s *Store) ensure(ctx context.Context, dir, cwd string, exclude []byte, extra []string) error {
	shadow := shadowOf(dir)
	if err := os.MkdirAll(s.root, 0o700); err != nil {
		return fmt.Errorf("snapshot: %w", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("snapshot: %w", err)
	}
	if _, err := os.Stat(filepath.Join(shadow, "HEAD")); err != nil {
		args := append(safeConfig(), "init", "-q", "--bare", shadow)
		if _, err := run(ctx, readOnlyEnv(), dir, nil, args...); err != nil {
			return err
		}
		if err := os.Chmod(shadow, 0o700); err != nil {
			return fmt.Errorf("snapshot: %w", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "cwd"), []byte(cwd), 0o600); err != nil {
			return fmt.Errorf("snapshot: %w", err)
		}
	}
	info := filepath.Join(shadow, "info")
	if err := os.MkdirAll(info, 0o700); err != nil {
		return fmt.Errorf("snapshot: %w", err)
	}
	var ex []byte
	ex = append(ex, "# rex shadow store: the user's .git/info/exclude, then skipped big files\n"...)
	ex = append(ex, exclude...)
	if len(exclude) > 0 && exclude[len(exclude)-1] != '\n' {
		ex = append(ex, '\n')
	}
	for _, l := range extra {
		ex = append(ex, l...)
		ex = append(ex, '\n')
	}
	if err := os.WriteFile(filepath.Join(info, "exclude"), ex, 0o600); err != nil {
		return fmt.Errorf("snapshot: %w", err)
	}
	// info/attributes outranks every .gitattributes: no eol conversion,
	// clean/smudge filters, ident or re-encoding touches a captured file.
	attrs := "* -text -filter -ident -working-tree-encoding\n"
	if err := os.WriteFile(filepath.Join(info, "attributes"), []byte(attrs), 0o600); err != nil {
		return fmt.Errorf("snapshot: %w", err)
	}
	return nil
}
