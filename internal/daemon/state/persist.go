package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// Session state holds prompts, cwds and full agent output, so it is
// private to the user: directories 0700, files 0600. MkdirAll and OpenFile
// only apply these to what they create; TightenPerms fixes older trees.
const (
	dirPerm  os.FileMode = 0o700
	filePerm os.FileMode = 0o600
)

// HookFile is the per-session file agent hooks append state words to. It
// fails with ErrUnsafeSessionID for an id that is not a single path element.
func HookFile(root, id string) (string, error) {
	dir, err := safeSessionDir(root, id)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "hooks.log"), nil
}

// ErrUnsafeSessionID is returned when a session id would not resolve to a
// single directory directly under <root>/sessions.
var ErrUnsafeSessionID = errors.New("unsafe session id")

// ValidateSessionID reports whether id can name a session directory: it
// must be one path element, not empty, "." or "..". Ids arrive from clients
// (Subscribe, Delete, ...), so every path built from one is checked.
func ValidateSessionID(id string) error {
	_, err := safeSessionDir("", id)
	return err
}

// safeSessionDir returns <root>/sessions/<id>. It is the only way this
// package turns an id into a path: it refuses ids that are empty, ".",
// "..", contain a separator, or otherwise escape <root>/sessions, so a
// client-supplied "../.." can't read or write elsewhere and an empty id
// can't RemoveAll every session.
func safeSessionDir(root, id string) (string, error) {
	if id == "" || id == "." || id == ".." || !filepath.IsLocal(id) || filepath.Base(id) != id {
		return "", fmt.Errorf("%w: %q", ErrUnsafeSessionID, id)
	}
	base := filepath.Join(root, "sessions")
	dir := filepath.Join(base, id)
	if filepath.Dir(dir) != base {
		return "", fmt.Errorf("%w: %q", ErrUnsafeSessionID, id)
	}
	return dir, nil
}

// SessionDir returns <root>/sessions/<id>, failing with ErrUnsafeSessionID
// for an id that is not a single path element.
func SessionDir(root, id string) (string, error) { return safeSessionDir(root, id) }

// RemoveSessionDir deletes a session's persisted directory (transcript + meta).
// Used on session delete so the session doesn't reappear after a daemon restart.
// Prefer RemoveSession when the *Session is at hand: it also fences off
// concurrent WriteMeta calls.
func RemoveSessionDir(root, id string) error {
	dir, err := safeSessionDir(root, id)
	if err != nil {
		slog.Error("state: refusing unsafe session dir", "id", id, "err", err)
		return err
	}
	return os.RemoveAll(dir)
}

// RemoveSession deletes a removed session's directory while holding its meta
// lock, so an in-flight WriteMeta finishes first and later ones see removed
// and skip. Call after Store.Remove.
func RemoveSession(root string, s *Session) error {
	s.metaMu.Lock()
	defer s.metaMu.Unlock()
	return RemoveSessionDir(root, s.ID)
}

// metaFile is meta.json: the wire summary plus what only the daemon keeps.
type metaFile struct {
	protocol.SessionSummary
	// AuditWritten is Session.AuditWritten.
	AuditWritten bool `json:"audit_written,omitempty"`
}

// WriteMeta persists a session's metadata atomically. Writers are serialized
// per session and each uses its own temp file; a session already removed from
// the store is skipped so a late write can't recreate its directory.
func WriteMeta(root string, s *Session) error {
	s.metaMu.Lock()
	defer s.metaMu.Unlock()
	if s.isRemoved() {
		slog.Debug("state: write_meta skipped (removed)", "session", s.ID)
		return nil
	}
	dir, err := safeSessionDir(root, s.ID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	final := filepath.Join(dir, "meta.json")

	// Snapshot under metaMu: whichever writer takes the lock last also takes
	// the newest snapshot, so the file on disk never regresses.
	sum := s.Summary()
	b, err := json.MarshalIndent(metaFile{SessionSummary: sum, AuditWritten: s.AuditIsWritten()}, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal meta: %w", err)
	}
	tmp, err := os.CreateTemp(dir, "meta-*.tmp")
	if err != nil {
		return fmt.Errorf("create tmp meta: %w", err)
	}
	tmpName := tmp.Name()
	_, werr := tmp.Write(b)
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("write tmp meta: %w", errors.Join(werr, cerr))
	}
	if err := os.Chmod(tmpName, filePerm); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("chmod tmp meta: %w", err)
	}
	if err := os.Rename(tmpName, final); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("rename meta: %w", err)
	}
	return nil
}

// LoadMeta loads one session's metadata.
func LoadMeta(root, id string) (*Session, error) {
	dir, err := safeSessionDir(root, id)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "meta.json")
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read meta %s: %w", path, err)
	}
	var mf metaFile
	if err := json.Unmarshal(b, &mf); err != nil {
		return nil, fmt.Errorf("parse meta %s: %w", path, err)
	}
	sess := fromSummary(mf.SessionSummary)
	sess.AuditWritten = mf.AuditWritten
	return sess, nil
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
			slog.Warn("state: skipping unreadable session meta", "dir", e.Name(), "err", err)
			continue
		}
		// The id inside meta.json drives later deletes; it must name this
		// directory or a corrupt file could point RemoveAll somewhere else.
		if sess.ID != e.Name() {
			slog.Warn("state: meta id/dir mismatch; skipped", "dir", e.Name(), "id", sess.ID)
			continue
		}
		switch sess.State {
		case protocol.StateQueued, protocol.StateWorking, protocol.StateNeedsInput:
			sess.State = protocol.StateCrashed
		case protocol.StateDone, protocol.StateFailed, protocol.StateCrashed:
			// Terminal states survive a daemon restart unchanged.
		}
		sess.WorkingSince = time.Time{}
		// Older files, and sessions just marked crashed, have no end time;
		// the last event is the best estimate. Written on the next WriteMeta.
		if isTerminal(sess.State) && sess.EndedAt.IsZero() {
			sess.EndedAt = sess.LastEventAt
		}
		out = append(out, sess)
	}
	return out, nil
}

// TranscriptFile is a kept-open transcript handle for the lifetime of one PTY session.
// Reuse avoids open/close syscalls on every PTY read chunk. Writes are
// unbuffered: the summarizer and attach replay read the file while the session
// is live, so bytes must hit disk on every chunk.
type TranscriptFile struct {
	f *os.File
}

// OpenTranscript opens (or creates) transcript.log for repeated appends. The
// file is made owner-only even if an older daemon created it.
func OpenTranscript(root, id string) (*TranscriptFile, error) {
	dir, err := safeSessionDir(root, id)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", dir, err)
	}
	f, err := os.OpenFile(filepath.Join(dir, "transcript.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, filePerm)
	if err != nil {
		return nil, fmt.Errorf("open transcript: %w", err)
	}
	if err := f.Chmod(filePerm); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("chmod transcript: %w", err)
	}
	return &TranscriptFile{f: f}, nil
}

// CreateHookFile creates the session's hook state file (owner-only, empty)
// if it does not exist yet and returns its path. Agent hooks append to it;
// pre-creating it means the agent never picks its permissions.
func CreateHookFile(root, id string) (string, error) {
	dir, err := safeSessionDir(root, id)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", dir, err)
	}
	path := filepath.Join(dir, "hooks.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, filePerm)
	if err != nil {
		return "", fmt.Errorf("create hook file: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("close hook file: %w", err)
	}
	return path, nil
}

// ResetHookFile empties an existing session's hook state file (owner-only)
// and returns its path, so a respawned agent's adapter does not replay the
// previous run's lines. Unlike CreateHookFile it never creates the session
// directory: a session deleted while a resume was in flight must not come
// back as an orphan directory.
func ResetHookFile(root, id string) (string, error) {
	dir, err := safeSessionDir(root, id)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "hooks.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, filePerm)
	if err != nil {
		return "", fmt.Errorf("reset hook file: %w", err)
	}
	if err := f.Chmod(filePerm); err != nil {
		_ = f.Close()
		return "", fmt.Errorf("chmod hook file: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("close hook file: %w", err)
	}
	return path, nil
}

// TightenPerms makes an existing state tree owner-only: root, root/sessions
// and every session dir become 0700, every regular file directly in them
// 0600. Older daemons created 0755 dirs and 0644 files. root itself may be a
// symlink (it is the configured dir), but symlinks inside are skipped, since
// Chmod would follow them out of the tree. A missing root is not an error.
// It keeps going past individual failures and returns them joined.
//
// On Windows, mode bits only toggle the read-only attribute, which these
// modes leave cleared; access there is governed by the profile's ACLs.
func TightenPerms(root string) error {
	fi, err := os.Stat(root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat %s: %w", root, err)
	}
	if !fi.IsDir() {
		return fmt.Errorf("state dir %s is not a directory", root)
	}
	errs := []error{chmodErr(root, dirPerm)}
	sessions := filepath.Join(root, "sessions")
	if _, err := os.Lstat(sessions); errors.Is(err, fs.ErrNotExist) {
		return errors.Join(errs...)
	}
	errs = append(errs, tightenDir(sessions))
	entries, err := os.ReadDir(sessions)
	if err != nil {
		return errors.Join(append(errs, fmt.Errorf("read %s: %w", sessions, err))...)
	}
	for _, e := range entries {
		if e.IsDir() {
			errs = append(errs, tightenDir(filepath.Join(sessions, e.Name())))
		}
	}
	return errors.Join(errs...)
}

// tightenDir makes dir 0700 and each regular file directly in it 0600.
func tightenDir(dir string) error {
	errs := []error{chmodErr(dir, dirPerm)}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return errors.Join(append(errs, fmt.Errorf("read %s: %w", dir, err))...)
	}
	for _, e := range entries {
		if e.Type().IsRegular() {
			errs = append(errs, chmodErr(filepath.Join(dir, e.Name()), filePerm))
		}
	}
	return errors.Join(errs...)
}

func chmodErr(path string, mode os.FileMode) error {
	if err := os.Chmod(path, mode); err != nil {
		return fmt.Errorf("chmod %s: %w", path, err)
	}
	return nil
}

// Write appends bytes to the transcript.
func (t *TranscriptFile) Write(b []byte) error {
	if _, err := t.f.Write(b); err != nil {
		return fmt.Errorf("write transcript: %w", err)
	}
	return nil
}

// Close closes the file.
func (t *TranscriptFile) Close() error {
	if err := t.f.Close(); err != nil {
		return fmt.Errorf("close transcript: %w", err)
	}
	return nil
}

// AppendTranscript appends bytes to the session's transcript.log, creating dirs as needed.
// For a long-lived PTY session prefer OpenTranscript and TranscriptFile.Write.
func AppendTranscript(root, id string, b []byte) error {
	tf, err := OpenTranscript(root, id)
	if err != nil {
		return err
	}
	defer tf.Close()
	return tf.Write(b)
}

// TranscriptTail returns the last max bytes of a session's transcript.log
// (or all of it, if smaller). Missing files return (nil, nil).
func TranscriptTail(root, id string, max int) ([]byte, error) {
	dir, err := safeSessionDir(root, id)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "transcript.log")
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
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, fmt.Errorf("read transcript: %w", err)
	}
	return buf[:n], nil
}

// maxSkills mirrors the per-session cap of the skills package, which state
// does not import.
const maxSkills = 16

// sanitizeSkills makes persisted skill names display-safe and bounded.
// meta.json is not trusted: the names are validated again, strictly, when
// they are used to stage a plugin.
func sanitizeSkills(in []string) []string {
	var out []string
	for _, n := range in {
		if len(out) == maxSkills {
			break
		}
		if n = protocol.SanitizeDisplayText(n, 64); n != "" {
			out = append(out, n)
		}
	}
	return out
}

// fromSummary rebuilds a session from its persisted summary. Free-text
// display fields are sanitized again: meta written before ingest
// sanitization (S-03) may still hold escapes or newlines.
func fromSummary(sum protocol.SessionSummary) *Session {
	// meta.json is not trusted for argv: keep only a valid resume id.
	resumeID, _ := protocol.NormalizeResumeID(sum.ResumeID)
	// Likewise the loop id, which names files elsewhere.
	loopID, loopIter := "", 0
	if protocol.ValidLoopID(sum.LoopID) && sum.LoopIter > 0 && sum.LoopIter <= 1000 {
		loopID, loopIter = sum.LoopID, sum.LoopIter
	}
	// And the workspace id, which names a registry entry.
	wsID := ""
	if protocol.ValidWorkspaceID(sum.WorkspaceID) {
		wsID = sum.WorkspaceID
	}
	return &Session{
		WorkspaceID: wsID,
		ID:          sum.ID, ShortID: sum.ShortID, ToolID: sum.ToolID, ModelID: sum.ModelID,
		Effort: sum.Effort, Slug: sum.Slug, CWD: sum.CWD,
		Title:         protocol.SanitizeDisplayText(sum.Title, protocol.MaxTitleRunes),
		Description:   protocol.SanitizeDisplayText(sum.Description, protocol.MaxDescriptionRunes),
		DescriptionAt: sum.DescriptionAt,
		Fleet:         protocol.SanitizeDisplayText(sum.Fleet, protocol.MaxFleetRunes),
		State:         sum.State, StartedAt: sum.StartedAt, LastEventAt: sum.LastEventAt,
		LastLine: sum.LastLine, ExitCode: sum.ExitCode,
		Tokens: sum.Tokens, OutputBytes: sum.OutputBytes,
		Name: sum.Name, RetireAt: sum.RetireAt, RetiredAt: sum.RetiredAt,
		WorkingSince: sum.WorkingSince, EndedAt: sum.EndedAt,
		ResumeID: resumeID, TrustFolder: sum.TrustFolder,
		Skills: sanitizeSkills(sum.Skills),
		LoopID: loopID, LoopIter: loopIter,
	}
}
