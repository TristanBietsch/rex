// Package audit keeps the audit trail: one JSON line per finished session
// (who ran what, how it ended, how long it took, the first line of the
// prompt) in an append-only file. Sessions inside a workspace write to
// <root>/.rex/audit.jsonl; every other session writes to the global
// ~/.local/share/rex/audit.jsonl.
//
// The files are private (0600), never followed through a symlink, and read
// defensively: a torn last line (a crash, or two machines appending to one
// synced file) is skipped and duplicates are dropped. A record holds the
// prompt's first line; the whole prompt only when the user opted in.
package audit

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

const (
	// MaxFirstLineRunes caps prompt_first_line.
	MaxFirstLineRunes = 200
	// MaxPromptRunes caps the opt-in full prompt; a longer one is cut.
	MaxPromptRunes = 20000
)

// DeletedWhileRunning is the description note of a session that was cleared
// before it ended; its state is recorded as failed.
const DeletedWhileRunning = "deleted while running"

// Record is one audit line: how one session run ended.
type Record struct {
	TS          time.Time `json:"ts"`
	WorkspaceID string    `json:"workspace_id,omitempty"`
	SessionID   string    `json:"session_id"`
	ShortID     string    `json:"short_id"`
	Name        string    `json:"name,omitempty"`
	Tool        string    `json:"tool"`
	Model       string    `json:"model,omitempty"`
	// PromptFirstLine is the first line of the initial prompt, sanitized and
	// cut to MaxFirstLineRunes. Empty when the daemon never saw a prompt.
	PromptFirstLine string `json:"prompt_first_line,omitempty"`
	// Prompt is the whole initial prompt. Only written with audit_full_prompt.
	Prompt      string    `json:"prompt,omitempty"`
	State       string    `json:"state"`
	StartedAt   time.Time `json:"started_at,omitzero"`
	EndedAt     time.Time `json:"ended_at"`
	DurationS   int64     `json:"duration_s"`
	ResumeID    string    `json:"resume_id,omitempty"`
	Description string    `json:"description,omitempty"`
}

// valid reports whether a parsed line is a usable record.
func (r Record) valid() bool {
	return r.SessionID != "" && !r.EndedAt.IsZero() && r.State != ""
}

// FirstLine is the first visible line of prompt, safe to print and at most
// MaxFirstLineRunes runes.
func FirstLine(prompt string) string {
	return protocol.SanitizeDisplayText(prompt, MaxFirstLineRunes)
}

// TrimPrompt bounds a full prompt to MaxPromptRunes runes and valid UTF-8.
func TrimPrompt(prompt string) string {
	if !utf8.ValidString(prompt) {
		prompt = string([]rune(prompt))
	}
	if utf8.RuneCountInString(prompt) > MaxPromptRunes {
		prompt = string([]rune(prompt)[:MaxPromptRunes])
	}
	return prompt
}

// FromSummary builds the record for a session that ended. promptFirst and
// prompt are what the daemon holds of the initial prompt ("" when none);
// prompt is stored only when fullPrompt is true. now stands in for an end
// time the summary lacks.
func FromSummary(sum protocol.SessionSummary, workspaceID, promptFirst, prompt string, fullPrompt bool, now time.Time) Record {
	ended := sum.EndedAt
	if ended.IsZero() {
		ended = now
	}
	var dur int64
	if !sum.StartedAt.IsZero() && ended.After(sum.StartedAt) {
		dur = int64(ended.Sub(sum.StartedAt) / time.Second)
	}
	name := sum.Name
	if name == "" {
		name = sum.Slug
	}
	state, desc := string(sum.State), protocol.SanitizeDisplayText(sum.Description, protocol.MaxDescriptionRunes)
	switch sum.State {
	case protocol.StateDone, protocol.StateFailed, protocol.StateCrashed:
	case protocol.StateQueued, protocol.StateWorking, protocol.StateNeedsInput:
		// Recorded at teardown while still live: the run was cut short.
		state = string(protocol.StateFailed)
		if desc == "" {
			desc = DeletedWhileRunning
		} else {
			desc += " (" + DeletedWhileRunning + ")"
		}
	}
	r := Record{
		TS:              now.UTC(),
		WorkspaceID:     workspaceID,
		SessionID:       sum.ID,
		ShortID:         sum.ShortID,
		Name:            protocol.SanitizeDisplayText(name, protocol.MaxTitleRunes),
		Tool:            sum.ToolID,
		Model:           sum.ModelID,
		PromptFirstLine: FirstLine(promptFirst),
		State:           state,
		StartedAt:       sum.StartedAt.UTC(),
		EndedAt:         ended.UTC(),
		DurationS:       dur,
		ResumeID:        sum.ResumeID,
		Description:     desc,
	}
	if fullPrompt && prompt != "" {
		r.Prompt = TrimPrompt(prompt)
	}
	return r
}

// ErrNotRegular is returned for an audit path that exists but is not a
// regular file (a symlink, a directory, a device). It is never followed.
var ErrNotRegular = errors.New("audit file is not a regular file")

// openRegular opens path with flags (0600 when created) without following a
// symlink or blocking on a FIFO, and checks the handle is a regular file.
// A refusal wraps ErrNotRegular.
func openRegular(path string, flags int) (*os.File, error) {
	f, err := os.OpenFile(path, flags|oNoFollow|oNonBlock, 0o600)
	if err != nil {
		if isLinkOpenErr(err) {
			return nil, fmt.Errorf("%w: %s", ErrNotRegular, path)
		}
		if fi, lerr := os.Lstat(path); lerr == nil && !fi.Mode().IsRegular() {
			return nil, fmt.Errorf("%w: %s", ErrNotRegular, path) // a directory, say
		}
		return nil, err
	}
	fi, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		_ = f.Close()
		return nil, fmt.Errorf("%w: %s", ErrNotRegular, path)
	}
	return f, nil
}

// appendMu serializes appends within the process, so concurrent records do
// not interleave their bytes.
var appendMu sync.Mutex

// Append writes r as one line at the end of path. The directory is created
// 0700 and the file 0600 (a wider mode on an existing file is tightened). A
// symlink or other non-regular file at path is refused.
func Append(path string, r Record) error {
	if !r.valid() {
		return fmt.Errorf("audit: incomplete record (session %q)", r.SessionID)
	}
	b, err := marshalLine(r)
	if err != nil {
		return err
	}
	appendMu.Lock()
	defer appendMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := openRegular(path, os.O_RDWR|os.O_APPEND|os.O_CREATE)
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
		if _, rerr := f.ReadAt(last[:], fi.Size()-1); rerr == nil && last[0] != '\n' {
			b = append([]byte{'\n'}, b...)
		}
	}
	_, err = f.Write(b) // one write: a crash leaves a whole line or a torn last one
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// marshalLine is r as one JSON line, newline included.
func marshalLine(r Record) ([]byte, error) {
	b, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}
