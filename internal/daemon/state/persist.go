package state

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// Session state is owner-only: a transcript holds everything an agent
// printed, meta.json the session's title and working directory.
const (
	dirPerm  os.FileMode = 0o700
	filePerm os.FileMode = 0o600
)

// SecureSessionsDir creates <root>/sessions owner-only, and tightens it when
// an older daemon created it world-readable. Other local users then can't
// reach any session file, whatever its own mode.
func SecureSessionsDir(root string) error {
	dir := filepath.Join(root, "sessions")
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	if err := os.Chmod(dir, dirPerm); err != nil {
		return fmt.Errorf("chmod %s: %w", dir, err)
	}
	return nil
}

// sessionDir returns ~/.local/share/rex/sessions/<id> built from a state-dir root.
// HookFile is the per-session file agent hooks append state words to.
func HookFile(root, id string) string {
	return filepath.Join(sessionDir(root, id), "hooks.log")
}

func sessionDir(root, id string) string {
	return filepath.Join(root, "sessions", id)
}

// ErrUnsafeSessionID is returned when a session id would not resolve to a
// single directory directly under <root>/sessions.
var ErrUnsafeSessionID = errors.New("unsafe session id")

// safeSessionDir is sessionDir for destructive operations. It refuses ids
// that are empty, ".", "..", contain a separator, or otherwise escape
// <root>/sessions — an empty id would otherwise RemoveAll every session.
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
	b, err := json.MarshalIndent(sum, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal meta: %w", err)
	}
	// CreateTemp makes the file 0600, which is what meta.json should be.
	tmp, err := os.CreateTemp(dir, "meta-*.tmp")
	if err != nil {
		return fmt.Errorf("create tmp meta: %w", err)
	}
	tmpName := tmp.Name()
	// Sync before the rename: otherwise a crash can leave meta.json renamed
	// into place but empty, and LoadAll would drop the session.
	_, werr := tmp.Write(b)
	serr := tmp.Sync()
	cerr := tmp.Close()
	if werr != nil || serr != nil || cerr != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("write tmp meta: %w", errors.Join(werr, serr, cerr))
	}
	if err := os.Rename(tmpName, final); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("rename meta: %w", err)
	}
	syncDir(dir)
	return nil
}

// syncDir makes a rename in dir durable. The new meta.json is already in
// place, so a failure (some filesystems refuse to sync a directory) is only
// logged. Windows can't sync a directory handle, and NTFS journals renames.
func syncDir(dir string) {
	if runtime.GOOS == "windows" {
		return
	}
	d, err := os.Open(dir)
	if err == nil {
		err = d.Sync()
		_ = d.Close()
	}
	if err != nil {
		slog.Debug("state: sync dir failed", "dir", dir, "err", err)
	}
}

// Caps for display fields loaded from disk. They match what the daemon
// accepts at intake; older daemons stored these fields unchecked.
const (
	maxLoadedTitleRunes       = 512
	maxLoadedFleetRunes       = 64
	maxLoadedDescriptionRunes = 200
	maxLoadedLastLineRunes    = 512
)

// LoadMeta loads one session's metadata. Display fields are re-sanitized:
// meta.json may predate intake sanitizing, or have been edited by hand.
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
	var sum protocol.SessionSummary
	if err := json.Unmarshal(b, &sum); err != nil {
		return nil, fmt.Errorf("parse meta %s: %w", path, err)
	}
	sum.Title = displayField(sum.Title, maxLoadedTitleRunes)
	sum.Fleet = displayField(sum.Fleet, maxLoadedFleetRunes)
	sum.Name = displayField(sum.Name, protocol.MaxSessionNameRunes)
	sum.Description = displayField(sum.Description, maxLoadedDescriptionRunes)
	sum.LastLine = displayField(sum.LastLine, maxLoadedLastLineRunes)
	return fromSummary(sum), nil
}

// displayField makes a persisted string safe for one board row
// (protocol.SingleLine) and cuts it to at most max runes.
func displayField(s string, max int) string {
	s = protocol.SingleLine(s)
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return strings.TrimRight(string([]rune(s)[:max]), " ")
}

// LoadAll loads every session under root/sessions/. Any session whose persisted
// state was "working", "queued", or "needs_input" gets reloaded as "crashed"
// because the live PTY didn't survive whatever caused the daemon to stop.
// It also deletes temp files a crashed WriteMeta left behind, so call it at
// startup, before any supervisor writes meta.
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
		removeStaleMetaTemps(filepath.Join(dir, e.Name()))
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
		}
		sess.WorkingSince = time.Time{}
		out = append(out, sess)
	}
	return out, nil
}

// removeStaleMetaTemps deletes meta-*.tmp files that a WriteMeta interrupted
// between create and rename left in a session dir.
func removeStaleMetaTemps(dir string) {
	tmps, _ := filepath.Glob(filepath.Join(dir, "meta-*.tmp"))
	for _, p := range tmps {
		if err := os.Remove(p); err != nil {
			slog.Warn("state: remove stale meta temp failed", "path", p, "err", err)
			continue
		}
		slog.Info("state: removed stale meta temp", "path", p)
	}
}

// transcriptRotateBytes caps transcript.log. A write that would cross it
// first renames the file to transcript.log.1 (replacing the previous one), so
// a session keeps at most about twice this on disk. A var for tests.
var transcriptRotateBytes int64 = 64 << 20

// TranscriptFile is a kept-open transcript handle for the lifetime of one PTY session.
// Reuse avoids open/close syscalls on every PTY read chunk. Writes are
// unbuffered: the summarizer and attach replay read the file while the session
// is live, so bytes must hit disk on every chunk.
type TranscriptFile struct {
	f    *os.File
	path string
	size int64
	// rotateAt is the size at which Write rotates. A failed rotation pushes
	// it out so the rename isn't retried on every chunk.
	rotateAt int64
}

// OpenTranscript opens (or creates) transcript.log for repeated appends.
func OpenTranscript(root, id string) (*TranscriptFile, error) {
	dir, err := safeSessionDir(root, id)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", dir, err)
	}
	path := filepath.Join(dir, "transcript.log")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, filePerm)
	if err != nil {
		return nil, fmt.Errorf("open transcript: %w", err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("stat transcript: %w", err)
	}
	return &TranscriptFile{f: f, path: path, size: info.Size(), rotateAt: transcriptRotateBytes}, nil
}

// Write appends bytes to the transcript, rotating it first at the size cap.
func (t *TranscriptFile) Write(b []byte) error {
	if t.size > 0 && t.size+int64(len(b)) > t.rotateAt {
		if err := t.rotate(); err != nil {
			return err
		}
	}
	n, err := t.f.Write(b)
	t.size += int64(n)
	if err != nil {
		return fmt.Errorf("write transcript: %w", err)
	}
	return nil
}

// rotate moves transcript.log to transcript.log.1 and starts a new one. The
// file is closed first because Windows can't rename an open file. A failed
// rename (a reader holding the file open on Windows) is logged and retried
// later; only failing to reopen the transcript is an error.
func (t *TranscriptFile) rotate() error {
	_ = t.f.Close()
	if err := os.Rename(t.path, t.path+".1"); err != nil {
		slog.Warn("state: transcript rotate failed; retrying later", "path", t.path, "err", err)
		t.rotateAt = t.size + transcriptRotateBytes/16
	} else {
		t.size = 0
		t.rotateAt = transcriptRotateBytes
	}
	f, err := os.OpenFile(t.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, filePerm)
	if err != nil {
		return fmt.Errorf("reopen transcript: %w", err)
	}
	t.f = f
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

// TranscriptTail returns the last max bytes of a session's transcript (or all
// of it, if smaller), continuing into transcript.log.1 when the current file
// was rotated recently. Missing files return (nil, nil). A tail cut from the
// middle starts at a safe boundary (see replayStart), so it can be up to max
// bytes shorter; attach replays it into a terminal emulator.
func TranscriptTail(root, id string, max int) ([]byte, error) {
	dir, err := safeSessionDir(root, id)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "transcript.log")
	buf, cut, err := readTail(path, max)
	if err != nil {
		return nil, err
	}
	if !cut && len(buf) < max {
		prev, prevCut, err := readTail(path+".1", max-len(buf))
		if err != nil {
			return nil, err
		}
		if len(prev) > 0 {
			buf, cut = append(prev, buf...), prevCut
		}
	}
	if cut {
		buf = buf[replayStart(buf):]
	}
	if len(buf) == 0 {
		return nil, nil
	}
	return buf, nil
}

// readTail returns the last max bytes of path and whether the file holds
// more than that. A missing file reads as empty.
func readTail(path string, max int) ([]byte, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("open transcript: %w", err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, false, fmt.Errorf("stat transcript: %w", err)
	}
	size := info.Size()
	n := min(int64(max), size)
	if n <= 0 {
		return nil, size > 0, nil
	}
	if _, err := f.Seek(-n, io.SeekEnd); err != nil {
		return nil, false, fmt.Errorf("seek transcript: %w", err)
	}
	buf := make([]byte, n)
	read, err := io.ReadFull(f, buf)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, false, fmt.Errorf("read transcript: %w", err)
	}
	return buf[:read], size > n, nil
}

// replayStart returns where a tail cut from the middle of a transcript can
// begin without starting inside an escape sequence or a UTF-8 rune: just
// after the first newline or at the first ESC, whichever comes first, else
// at the first rune start. Replaying from mid-sequence would feed the
// emulator a broken escape and garble the screen.
func replayStart(b []byte) int {
	start := -1
	if i := bytes.IndexByte(b, '\n'); i >= 0 {
		start = i + 1
	}
	if i := bytes.IndexByte(b, 0x1b); i >= 0 && (start < 0 || i < start) {
		start = i
	}
	if start >= 0 {
		return start
	}
	for i := 0; i < len(b) && i < utf8.UTFMax; i++ {
		if utf8.RuneStart(b[i]) {
			return i
		}
	}
	return 0
}

func fromSummary(sum protocol.SessionSummary) *Session {
	return &Session{
		ID: sum.ID, ShortID: sum.ShortID, ToolID: sum.ToolID, ModelID: sum.ModelID,
		Effort: sum.Effort, Slug: sum.Slug, Title: sum.Title, CWD: sum.CWD,
		State: sum.State, StartedAt: sum.StartedAt, LastEventAt: sum.LastEventAt,
		LastLine: sum.LastLine, Description: sum.Description, ExitCode: sum.ExitCode,
		Tokens: sum.Tokens, OutputBytes: sum.OutputBytes,
		Fleet: sum.Fleet, Name: sum.Name, RetireAt: sum.RetireAt, RetiredAt: sum.RetiredAt,
		WorkingSince: sum.WorkingSince,
	}
}
