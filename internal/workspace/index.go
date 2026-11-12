package workspace

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// IndexFileName is the per-workspace session index inside DirName.
const IndexFileName = "sessions.jsonl"

// IndexLine is one line of <root>/.rex/sessions.jsonl: a session that ran in
// the workspace. Written at spawn (no ended_at) and again at the session's
// first terminal state; a reader keeps the newest line per session_id.
type IndexLine struct {
	SessionID string    `json:"session_id"`
	ShortID   string    `json:"short_id"`
	Tool      string    `json:"tool"`
	Model     string    `json:"model"`
	Name      string    `json:"name"`
	CWD       string    `json:"cwd"`
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at,omitzero"`
	State     string    `json:"state"`
	ResumeID  string    `json:"resume_id,omitempty"`
}

var indexMu sync.Mutex

// AppendIndex appends l to w's session index (0600, O_APPEND). It never
// creates .rex/: the directory is checked as Find does (an unsafe one wraps
// ErrUnsafe), and a symlink or other non-regular file at the index path is
// refused with ErrUnsafe.
func AppendIndex(w *Workspace, l IndexLine) error {
	if l.SessionID == "" {
		return errors.New("workspace: index line needs a session id")
	}
	if w == nil || w.Root == "" || !filepath.IsAbs(w.Root) {
		return errors.New("workspace: index needs a workspace root")
	}
	rexDir := filepath.Join(w.Root, DirName)
	if err := checkDir(rexDir); err != nil {
		if errors.Is(err, errNotDir) {
			return fmt.Errorf("%w: %s is not a directory", ErrUnsafe, rexDir)
		}
		return err
	}
	b, err := json.Marshal(l)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	path := filepath.Join(rexDir, IndexFileName)

	indexMu.Lock()
	defer indexMu.Unlock()
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE|oNoFollow|oNonBlock, 0o600)
	if err != nil {
		if isLinkOpenErr(err) {
			return fmt.Errorf("%w: %s is a symlink", ErrUnsafe, path)
		}
		if fi, lerr := os.Lstat(path); lerr == nil && !fi.Mode().IsRegular() {
			return fmt.Errorf("%w: %s is not a regular file", ErrUnsafe, path)
		}
		return err
	}
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		_ = f.Close()
		return fmt.Errorf("%w: %s is not a regular file", ErrUnsafe, path)
	}
	if err := f.Chmod(0o600); err != nil && !errors.Is(err, os.ErrPermission) {
		_ = f.Close()
		return err
	}
	_, err = f.Write(b)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}
