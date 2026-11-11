package snapshot

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
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
func readLedger(dir string) ([]Entry, error) {
	b, err := os.ReadFile(ledgerOf(dir))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("snapshot: ledger: %w", err)
	}
	var out []Entry
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for n := 1; sc.Scan(); n++ {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var e Entry
		if err := json.Unmarshal(line, &e); err != nil {
			return nil, fmt.Errorf("snapshot: ledger line %d: %w", n, err)
		}
		out = append(out, e)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("snapshot: ledger: %w", err)
	}
	return out, nil
}

func appendLedger(dir string, e Entry) error {
	b, err := json.Marshal(e)
	if err != nil {
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
	if err := os.WriteFile(tmp, buf.Bytes(), 0o600); err != nil {
		return fmt.Errorf("snapshot: ledger: %w", err)
	}
	if err := os.Rename(tmp, ledgerOf(dir)); err != nil {
		return fmt.Errorf("snapshot: ledger: %w", err)
	}
	return nil
}

// progress is one session's place in its turn history, folded from the
// ledger. cursor is how many turns are currently applied; starts holds the
// start tree of each turn still in the history; undone is the stack of
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
			p.last = e.Tree
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
