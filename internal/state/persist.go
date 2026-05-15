package state

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/tristanbietsch/rex/internal/protocol"
)

// sessionDir returns ~/.local/share/rex/sessions/<id> built from a state-dir root.
func sessionDir(root, id string) string {
	return filepath.Join(root, "sessions", id)
}

// RemoveSessionDir deletes a session's persisted directory (transcript + meta).
// Used on session delete so the session doesn't reappear after a daemon restart.
func RemoveSessionDir(root, id string) error {
	return os.RemoveAll(sessionDir(root, id))
}

// WriteMeta persists a session's metadata atomically.
func WriteMeta(root string, s *Session) error {
	dir := sessionDir(root, s.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	final := filepath.Join(dir, "meta.json")
	tmp := final + ".tmp"

	sum := s.Summary()
	b, err := json.MarshalIndent(sum, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal meta: %w", err)
	}
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return fmt.Errorf("write tmp meta: %w", err)
	}
	if err := os.Rename(tmp, final); err != nil {
		return fmt.Errorf("rename meta: %w", err)
	}
	return nil
}

// LoadMeta loads one session's metadata.
func LoadMeta(root, id string) (*Session, error) {
	path := filepath.Join(sessionDir(root, id), "meta.json")
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read meta %s: %w", path, err)
	}
	var sum protocol.SessionSummary
	if err := json.Unmarshal(b, &sum); err != nil {
		return nil, fmt.Errorf("parse meta %s: %w", path, err)
	}
	return fromSummary(sum), nil
}

// LoadAll loads every session under root/sessions/. Any session whose persisted
// state was "working", "queued", or "needs_input" gets reloaded as "crashed"
// because the live PTY didn't survive whatever caused the daemon to stop.
func LoadAll(root string) ([]*Session, error) {
	dir := filepath.Join(root, "sessions")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read sessions dir: %w", err)
	}
	out := make([]*Session, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sess, err := LoadMeta(root, e.Name())
		if err != nil {
			// Skip unreadable sessions rather than fail the whole daemon.
			continue
		}
		switch sess.State {
		case protocol.StateQueued, protocol.StateWorking, protocol.StateNeedsInput:
			sess.State = protocol.StateCrashed
		}
		out = append(out, sess)
	}
	return out, nil
}

// AppendTranscript appends bytes to the session's transcript.log, creating dirs as needed.
func AppendTranscript(root, id string, b []byte) error {
	dir := sessionDir(root, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	f, err := os.OpenFile(filepath.Join(dir, "transcript.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open transcript: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(b); err != nil {
		return fmt.Errorf("write transcript: %w", err)
	}
	return nil
}

// TranscriptTail returns the last max bytes of a session's transcript.log
// (or all of it, if smaller). Missing files return (nil, nil).
func TranscriptTail(root, id string, max int) ([]byte, error) {
	path := filepath.Join(sessionDir(root, id), "transcript.log")
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open transcript: %w", err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat transcript: %w", err)
	}
	size := info.Size()
	if int64(max) > size {
		max = int(size)
	}
	if max <= 0 {
		return nil, nil
	}
	if _, err := f.Seek(-int64(max), io.SeekEnd); err != nil {
		return nil, fmt.Errorf("seek transcript: %w", err)
	}
	buf := make([]byte, max)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.ErrUnexpectedEOF {
		return nil, fmt.Errorf("read transcript: %w", err)
	}
	return buf[:n], nil
}

func fromSummary(sum protocol.SessionSummary) *Session {
	return &Session{
		ID: sum.ID, ShortID: sum.ShortID, ToolID: sum.ToolID, ModelID: sum.ModelID,
		Effort: sum.Effort, Slug: sum.Slug, Title: sum.Title, CWD: sum.CWD,
		State: sum.State, StartedAt: sum.StartedAt, LastEventAt: sum.LastEventAt,
		LastLine: sum.LastLine, ExitCode: sum.ExitCode,
	}
}
