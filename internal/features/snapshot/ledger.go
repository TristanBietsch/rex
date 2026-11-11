package snapshot

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Entry is one line of a folder store's turns.jsonl.
//
// Captures (start, end, safety, pre-redo) carry the tree. Operations (undo,
// redo, removed) move the session's cursor or mark it gone; an undo or redo
// line also carries the tree it restored.
type Entry struct {
	SessionID    string    `json:"session_id"`
	Turn         int       `json:"turn"`
	Phase        Phase     `json:"phase"`
	Tree         Tree      `json:"tree,omitempty"`
	At           time.Time `json:"at"`
	FilesChanged int       `json:"files_changed"`
}

// ErrLedgerCorrupt means a folder's ledger is damaged somewhere other than
// its last line. Read-only callers get it and change nothing; the next write
// path sets the file aside (see readLedgerMode).
var ErrLedgerCorrupt = errors.New("snapshot: the ledger of this folder is corrupt")

// ResetError says a folder's ledger was found corrupt and set aside, so its
// older turns can no longer be undone.
type ResetError struct {
	At   time.Time
	File string // the set-aside file's name, in the folder store
}

func (e *ResetError) Error() string {
	return fmt.Sprintf("history for this folder was reset at %s (the old ledger is kept as %s); older turns can't be undone",
		e.At.Local().Format(time.DateTime), e.File)
}

func markerOf(dir string) string { return filepath.Join(dir, "ledger-reset") }

// readReset returns the reset marker of a folder store, or nil.
func readReset(dir string) *ResetError {
	b, err := os.ReadFile(markerOf(dir))
	if err != nil {
		return nil //nolint:nilerr // no readable marker means no reset
	}
	at, file, _ := strings.Cut(strings.TrimSpace(string(b)), " ")
	t, err := time.Parse(time.RFC3339, at)
	if err != nil {
		if fi, serr := os.Stat(markerOf(dir)); serr == nil {
			t = fi.ModTime()
		}
	}
	return &ResetError{At: t, File: file}
}

// resetTime is when the folder store's ledger was last set aside, if ever:
// from the marker, else from a leftover .corrupt-* file.
func resetTime(dir string) (time.Time, bool) {
	if re := readReset(dir); re != nil {
		return re.At, true
	}
	if m, _ := filepath.Glob(ledgerOf(dir) + ".corrupt-*"); len(m) > 0 {
		if fi, err := os.Stat(m[0]); err == nil {
			return fi.ModTime(), true
		}
		return time.Now(), true
	}
	return time.Time{}, false
}

// readLedger is readLedgerMode for the write paths (the daemon's captures,
// undo, redo, retention): a corrupt ledger is set aside.
func readLedger(dir string) ([]Entry, error) { return readLedgerMode(dir, true) }

// peekLedger is readLedgerMode for read-only callers (rex doctor, Entries,
// a dry run): corruption is reported, nothing is renamed.
func peekLedger(dir string) ([]Entry, error) { return readLedgerMode(dir, false) }

// readLedgerMode returns the ledger's entries in order; a missing file is empty.
//
// A torn or malformed last line (a write cut short) is skipped with a
// warning. A malformed line anywhere else means the file cannot be trusted.
// With repair it is renamed to turns.jsonl.corrupt-<time>, a ledger-reset
// marker is written, and the folder starts a fresh ledger (its sessions'
// captures stay in the store but can no longer be undone). Without repair
// the error is ErrLedgerCorrupt and nothing changes.
func readLedgerMode(dir string, repair bool) ([]Entry, error) {
	b, err := os.ReadFile(ledgerOf(dir))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("snapshot: ledger: %w", err)
	}
	lines := bytes.Split(b, []byte{'\n'})
	last := -1
	for i, l := range lines {
		if len(bytes.TrimSpace(l)) > 0 {
			last = i
		}
	}
	var out []Entry
	for i, l := range lines {
		line := bytes.TrimSpace(l)
		if len(line) == 0 {
			continue
		}
		var e Entry
		if err := json.Unmarshal(line, &e); err != nil {
			if i == last {
				slog.Warn("snapshots: ignoring a torn last ledger line", "ledger", ledgerOf(dir), "err", err)
				continue
			}
			if !repair {
				return nil, fmt.Errorf("%w (line %d: %w)", ErrLedgerCorrupt, i+1, err)
			}
			now := time.Now()
			dst := fmt.Sprintf("%s.corrupt-%d", ledgerOf(dir), now.Unix())
			if rerr := os.Rename(ledgerOf(dir), dst); rerr != nil {
				return nil, fmt.Errorf("snapshot: ledger line %d is corrupt and could not be set aside: %w", i+1, rerr)
			}
			marker := now.UTC().Format(time.RFC3339) + " " + filepath.Base(dst) + "\n"
			if werr := os.WriteFile(markerOf(dir), []byte(marker), 0o600); werr != nil {
				slog.Warn("snapshots: could not write the ledger-reset marker", "err", werr)
			}
			slog.Warn("snapshots: corrupt ledger set aside; starting a new one", "moved_to", dst, "line", i+1, "err", err)
			return nil, nil
		}
		out = append(out, e)
	}
	return out, nil
}

// trimTornTail cuts a last line that is not a whole JSON line off the ledger,
// so an append starts on a fresh line.
func trimTornTail(path string) error {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) || len(b) == 0 {
		return nil
	}
	if err != nil {
		return err
	}
	end := len(b)
	if b[end-1] == '\n' {
		end--
	}
	start := bytes.LastIndexByte(b[:end], '\n') + 1
	tail := bytes.TrimSpace(b[start:end])
	if len(tail) == 0 || (b[len(b)-1] == '\n' && json.Valid(tail)) {
		return nil
	}
	if json.Valid(tail) { // whole but without its newline
		return os.WriteFile(path, append(b, '\n'), 0o600) //nolint:gosec // the store's own ledger path
	}
	return os.Truncate(path, int64(start))
}

func appendLedger(dir string, e Entry) error {
	b, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("snapshot: ledger: %w", err)
	}
	if err := trimTornTail(ledgerOf(dir)); err != nil {
		return fmt.Errorf("snapshot: ledger: %w", err)
	}
	f, err := os.OpenFile(ledgerOf(dir), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("snapshot: ledger: %w", err)
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		_ = f.Close()
		return fmt.Errorf("snapshot: ledger: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("snapshot: ledger: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("snapshot: ledger: %w", err)
	}
	return nil
}

// rewriteLedger replaces the ledger with entries (atomic rename).
func rewriteLedger(dir string, entries []Entry) error {
	var buf bytes.Buffer
	for _, e := range entries {
		b, err := json.Marshal(e)
		if err != nil {
			return fmt.Errorf("snapshot: ledger: %w", err)
		}
		buf.Write(b)
		buf.WriteByte('\n')
	}
	tmp := ledgerOf(dir) + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("snapshot: ledger: %w", err)
	}
	if _, err := f.Write(buf.Bytes()); err != nil {
		_ = f.Close()
		return fmt.Errorf("snapshot: ledger: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("snapshot: ledger: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("snapshot: ledger: %w", err)
	}
	if err := os.Rename(tmp, ledgerOf(dir)); err != nil {
		return fmt.Errorf("snapshot: ledger: %w", err)
	}
	return nil
}

// progress is one session's place in its turn history, folded from the
// ledger. cursor is how many turns are currently applied; starts holds the
// start tree of each turn still in the history (empty when its capture
// failed: the turn is not undoable); undone is the stack of
// safety captures redo pops.
type progress struct {
	cursor int
	starts map[int]Tree
	undone []Entry
	last   Tree // tree of the session's latest capture, the files_changed base
}

func fold(entries []Entry, session string) progress {
	p := progress{starts: map[int]Tree{}}
	for _, e := range entries {
		if e.SessionID != session {
			continue
		}
		switch e.Phase {
		case PhaseStart:
			// A new turn after an undo replaces the undone ones, as a new
			// commit replaces the branch tip: redo history is dropped.
			for t := range p.starts {
				if t >= e.Turn {
					delete(p.starts, t)
				}
			}
			p.starts[e.Turn] = e.Tree
			p.cursor = e.Turn
			p.undone = nil
			if e.Tree != "" {
				p.last = e.Tree
			}
		case PhaseEnd, PhasePreRedo:
			p.last = e.Tree
		case PhaseSafety:
			p.undone = append(p.undone, e)
			p.last = e.Tree
		case phaseUndo:
			p.cursor = e.Turn
		case phaseRedo:
			if n := len(p.undone); n > 0 {
				p.undone = p.undone[:n-1]
			}
			p.cursor = e.Turn
		case phaseRemoved:
		}
	}
	return p
}

// Entries returns the ledger of cwd's folder store in order (nil when the
// folder has no store).
func (s *Store) Entries(cwd string) ([]Entry, error) {
	if err := checkCWD(cwd); err != nil {
		return nil, err
	}
	dir := s.dirFor(cwd)
	defer s.lock(dir)()
	return peekLedger(dir)
}
