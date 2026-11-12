package audit

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"
)

// maxLineBytes is the longest line Read parses; a longer one is skipped.
// Room for a MaxPromptRunes prompt of 4-byte runes and its JSON escapes.
const maxLineBytes = 512 << 10

// maxReadBytes is how much of the end of one file Read parses.
const maxReadBytes = 8 << 20

// ErrTruncated is wrapped by the error Read reports for a file larger than
// maxReadBytes: its newest part was read, the older records were not.
var ErrTruncated = errors.New("audit file is large")

// Filter narrows what Read returns. A zero field matches everything.
type Filter struct {
	// Since keeps records that ended at or after it.
	Since time.Time
	// Tool and State keep records of exactly this tool id or state.
	Tool, State string
	// Workspace keeps records with this workspace id.
	Workspace string
}

func (f Filter) match(r Record) bool {
	switch {
	case !f.Since.IsZero() && r.EndedAt.Before(f.Since):
		return false
	case f.Tool != "" && r.Tool != f.Tool:
		return false
	case f.State != "" && r.State != f.State:
		return false
	case f.Workspace != "" && r.WorkspaceID != f.Workspace:
		return false
	}
	return true
}

// Read returns the records of every file in paths that match f, oldest
// first. A missing file is empty. A file that cannot be read (a symlink, a
// permission error) adds an error to the second result and the other files
// still count. Lines that are torn, too long or not records are skipped, and
// a record repeated across files or lines (same session_id and ended_at,
// as when two machines append to one synced file) counts once.
func Read(paths []string, f Filter) ([]Record, []error) {
	var (
		out  []Record
		errs []error
		seen = map[string]bool{}
	)
	for _, p := range paths {
		err := scanFile(p, func(r Record) {
			key := r.SessionID + "\x00" + r.EndedAt.UTC().Format(time.RFC3339Nano)
			if seen[key] {
				return
			}
			seen[key] = true
			if f.match(r) {
				out = append(out, r)
			}
		})
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", p, err))
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].EndedAt.Before(out[j].EndedAt) })
	return out, errs
}

// scanFile calls keep for each valid record of the file at path.
func scanFile(path string, keep func(Record)) (err error) {
	file, err := openRegular(path, os.O_RDONLY)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()
	var truncated error
	r := bufio.NewReaderSize(file, 64<<10)
	if fi, serr := file.Stat(); serr == nil && fi.Size() > maxReadBytes {
		// Only the newest part: start at the tail and drop the partial line.
		if _, err := file.Seek(fi.Size()-maxReadBytes, io.SeekStart); err != nil {
			return err
		}
		r.Reset(file)
		if _, err := r.ReadBytes('\n'); err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		truncated = fmt.Errorf("%w: only the newest %d MiB was read", ErrTruncated, maxReadBytes>>20)
	}
	defer func() {
		if err == nil {
			err = truncated
		}
	}()
	var line []byte
	skipping := false
	for {
		chunk, rerr := r.ReadSlice('\n')
		if len(chunk) > 0 && !skipping {
			if len(line)+len(chunk) > maxLineBytes {
				skipping, line = true, line[:0]
			} else {
				line = append(line, chunk...)
			}
		}
		if errors.Is(rerr, bufio.ErrBufferFull) {
			continue // the same line goes on
		}
		if !skipping {
			if t := bytes.TrimSpace(line); len(t) > 0 {
				var rec Record
				if json.Unmarshal(t, &rec) == nil && rec.valid() {
					keep(rec)
				}
			}
		}
		line, skipping = line[:0], false
		if rerr != nil {
			if errors.Is(rerr, io.EOF) {
				return nil
			}
			return rerr
		}
	}
}

// ParseSince turns "7d", "12h", "90m" or an RFC 3339 date or timestamp into
// a time before now. An empty s is the zero time (no limit).
func ParseSince(s string, now time.Time) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, nil
	}
	if n := len(s); n > 1 && s[n-1] == 'd' {
		var days int
		if _, err := fmt.Sscanf(s[:n-1], "%d", &days); err == nil && fmt.Sprint(days) == s[:n-1] && days >= 0 {
			return now.AddDate(0, 0, -days), nil
		}
	}
	if d, err := time.ParseDuration(s); err == nil && d >= 0 {
		return now.Add(-d), nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("--since %q: use 7d, 12h, 90m or a date like 2026-11-01", s)
}
