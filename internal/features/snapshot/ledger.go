package snapshot

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
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

// readLedger returns the ledger's entries in order; a missing file is empty.
//
// A torn or malformed last line (a write cut short) is skipped with a
// warning. A malformed line anywhere else means the file cannot be trusted:
// it is renamed to turns.jsonl.corrupt-<time> with a warning, and the folder
// starts a fresh ledger (its sessions' captures stay in the store but can
// no longer be undone).
func readLedger(dir string) ([]Entry, error) {
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
			dst := fmt.Sprintf("%s.corrupt-%d", ledgerOf(dir), time.Now().Unix())
			if rerr := os.Rename(ledgerOf(dir), dst); rerr != nil {
				return nil, fmt.Errorf("snapshot: ledger line %d is corrupt and could not be set aside: %w", i+1, rerr)
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
	return readLedger(dir)
}
