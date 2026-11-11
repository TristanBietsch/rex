package usage

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// LedgerVersion is the "v" of every ledger line.
const LedgerVersion = 1

// Record is one ledger line: a session's totals at one moment. The ledger is
// append-only; a session has many lines (periodic snapshots, then a final
// one), and the latest by At is the one that counts (Latest).
//
// A record holds no prompt, output or path: ids, names, counts and cost.
type Record struct {
	V         int    `json:"v"`
	SessionID string `json:"session_id"`
	Tool      string `json:"tool"`
	Model     string `json:"model,omitempty"`
	Tokens    int64  `json:"tokens"`
	// CostUSD is nil when no source reported a cost for the session.
	CostUSD *float64 `json:"cost_usd,omitempty"`
	// Estimated marks Tokens as rex's own estimate.
	Estimated bool      `json:"estimated,omitempty"`
	StartedAt time.Time `json:"started_at,omitzero"`
	At        time.Time `json:"at"`
	// Ended marks the session's final line.
	Ended bool `json:"ended,omitempty"`
}

// valid reports whether a parsed line is usable.
func (r Record) valid() bool {
	return r.SessionID != "" && !r.At.IsZero() && r.Tokens >= 0 && (r.CostUSD == nil || *r.CostUSD >= 0)
}

// Ledger is the append-only usage file. Safe for concurrent use within one
// process; the daemon is its only writer.
type Ledger struct {
	path string
	mu   sync.Mutex
}

// OpenLedger returns the ledger at path. It touches nothing: the file and
// its directory are created by the first Append.
func OpenLedger(path string) *Ledger { return &Ledger{path: path} }

// Path is the ledger file.
func (l *Ledger) Path() string { return l.path }

// ErrNotRegular is returned for a ledger path that exists but is not a
// regular file (a symlink, a directory, a device). It is never followed.
var ErrNotRegular = errors.New("usage ledger is not a regular file")

// checkRegular refuses an existing ledger path that is not a regular file.
// A missing file is fine. Callers hold l.mu.
func (l *Ledger) checkRegular() error {
	fi, err := os.Lstat(l.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("%w: %s", ErrNotRegular, l.path)
	}
	return nil
}

// Append writes r as one line. The directory is created 0700 and the file
// 0600 (a wider mode on an existing file is tightened first).
func (l *Ledger) Append(r Record) error {
	r.V = LedgerVersion
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(l.path), 0o700); err != nil {
		return err
	}
	if err := l.checkRegular(); err != nil {
		return err
	}
	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if err := f.Chmod(0o600); err != nil && !errors.Is(err, os.ErrPermission) {
		_ = f.Close()
		return err
	}
	// A crash can leave a torn last line with no newline; start on a fresh
	// line so the new record is not glued to it.
	if fi, serr := f.Stat(); serr == nil && fi.Size() > 0 {
		var last [1]byte
		if rf, rerr := os.Open(l.path); rerr == nil {
			if _, rerr = rf.ReadAt(last[:], fi.Size()-1); rerr == nil && last[0] != '\n' {
				b = append([]byte{'\n'}, b...)
			}
			_ = rf.Close()
		}
	}
	_, err = f.Write(b) // one write: a crash leaves a whole line or a torn last one
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// scan reads every line of the ledger, calling keep for each valid record in
// file order, and returns how many lines were unusable (corrupt JSON, a
// missing field). A missing file is an empty ledger.
func (l *Ledger) scan(keep func(Record)) (corrupt int, err error) {
	if err := l.checkRegular(); err != nil {
		return 0, err
	}
	f, err := os.Open(l.path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer f.Close()
	r := bufio.NewReader(f)
	for {
		line, rerr := r.ReadBytes('\n')
		if t := bytes.TrimSpace(line); len(t) > 0 {
			var rec Record
			if json.Unmarshal(t, &rec) != nil || !rec.valid() {
				corrupt++
			} else {
				keep(rec)
			}
		}
		if rerr != nil {
			if errors.Is(rerr, io.EOF) {
				return corrupt, nil
			}
			return corrupt, rerr
		}
	}
}

// Latest returns the latest record of every session: the one with the
// greatest At, the later line on a tie. Periodic lines of a live session
// therefore never double count. Corrupt lines are skipped with one warning
// per call.
func (l *Ledger) Latest() (map[string]Record, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := map[string]Record{}
	corrupt, err := l.scan(func(r Record) {
		if cur, ok := out[r.SessionID]; !ok || !r.At.Before(cur.At) {
			out[r.SessionID] = r
		}
	})
	if err != nil {
		return nil, err
	}
	if corrupt > 0 {
		slog.Warn("usage: skipped unreadable ledger lines", "path", l.path, "lines", corrupt)
	}
	return out, nil
}

// Trim rewrites the ledger without lines older than cutoff and without
// unreadable lines. A ledger that needs neither is left alone, and a missing
// one is not created. The rewrite is atomic (temp file, then rename) and the
// new file is 0600.
func (l *Ledger) Trim(cutoff time.Time) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	var keep []Record
	dropped := 0
	corrupt, err := l.scan(func(r Record) {
		if r.At.Before(cutoff) {
			dropped++
			return
		}
		keep = append(keep, r)
	})
	if err != nil {
		return err
	}
	if dropped == 0 && corrupt == 0 {
		return nil
	}
	if corrupt > 0 {
		slog.Warn("usage: dropped unreadable ledger lines while trimming", "path", l.path, "lines", corrupt)
	}
	tmp, err := os.CreateTemp(filepath.Dir(l.path), ".usage-*.tmp") // 0600
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // a no-op after a successful rename
	w := bufio.NewWriter(tmp)
	for _, r := range keep {
		b, _ := json.Marshal(r)
		_, _ = w.Write(append(b, '\n'))
	}
	if err := w.Flush(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write trimmed ledger: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), l.path)
}
