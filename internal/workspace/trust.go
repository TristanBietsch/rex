package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"
)

// trustVersion is the "v" of workspace-trust.json.
const trustVersion = 1

// maxTrustBytes caps workspace-trust.json; a larger file trusts nothing.
const maxTrustBytes = 1 << 20

// TrustEntry is one workspace the user trusted: the real folder (symlinks
// resolved), the id its workspace.json held then, and the SHA-256 of the
// .rex/config.lua the user trusted ("" when there was none).
type TrustEntry struct {
	ID        string    `json:"id"`
	Root      string    `json:"root"`
	SHA256    string    `json:"sha256"`
	TrustedAt time.Time `json:"trusted_at"`
}

// ConfigHash is the hex SHA-256 of a .rex/config.lua's bytes; "" for nil
// (no file).
func ConfigHash(b []byte) string {
	if b == nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

type trustFile struct {
	V       int          `json:"v"`
	Trusted []TrustEntry `json:"trusted"`
}

// TrustStore is ~/.local/share/rex/workspace-trust.json: the workspaces
// whose .rex/config.lua rex evaluates (rex ws trust). It lives outside every
// .rex/, so a cloned repo cannot ship its own trust. Trust binds the real
// folder, the id and the config.lua bytes together (TrustedHash): the same id in another folder (a copy of a
// committed workspace.json), another id in the trusted folder, or the
// trusted workspace moved elsewhere are all untrusted until trusted again.
//
// The file is read as hostile input like a .rex/ file (no symlink, private,
// yours): one that fails a check or does not parse trusts nothing, and
// Trust refuses to overwrite it. Trust and Revoke hold a cross-process
// lock (lockTrustFile) around their read-modify-write, so concurrent rex
// processes never lose each other's change.
type TrustStore struct {
	path string
	mu   sync.Mutex
}

// OpenTrust returns the trust store at path. It touches nothing: the file
// and its directory are created by the first Trust.
func OpenTrust(path string) (*TrustStore, error) {
	if path == "" || !filepath.IsAbs(path) {
		return nil, fmt.Errorf("workspace: trust file path %q is not absolute", path)
	}
	return &TrustStore{path: path}, nil
}

// Path is the trust file.
func (s *TrustStore) Path() string { return s.path }

// realRoot is root with symlinks resolved, so a folder reached through a
// link is the same folder.
func realRoot(root string) (string, error) {
	if !filepath.IsAbs(root) {
		return "", fmt.Errorf("workspace: root %q is not absolute", root)
	}
	r, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	return filepath.Clean(r), nil
}

func (s *TrustStore) read() (trustFile, error) {
	tf := trustFile{V: trustVersion}
	b, err := readPrivate(s.path, maxTrustBytes)
	if errors.Is(err, fs.ErrNotExist) {
		return tf, nil
	}
	if err != nil {
		return tf, err
	}
	var got trustFile
	if err := json.Unmarshal(b, &got); err != nil {
		return tf, fmt.Errorf("%w: %s: %w", ErrInvalid, s.path, err)
	}
	for _, e := range got.Trusted {
		if ValidID(e.ID) && filepath.IsAbs(e.Root) {
			e.Root = filepath.Clean(e.Root)
			tf.Trusted = append(tf.Trusted, e)
		}
	}
	return tf, nil
}

func (s *TrustStore) write(tf trustFile) error {
	tf.V = trustVersion
	b, err := json.MarshalIndent(tf, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	return writeFileAtomic(s.path, append(b, '\n'))
}

// Trusted reports whether w (its real folder and id) is trusted. Any
// problem reading the store answers false. It says nothing about the
// config.lua content: TrustedHash does.
func (s *TrustStore) Trusted(w *Workspace) bool {
	_, ok := s.TrustedHash(w)
	return ok
}

// TrustedHash returns the config.lua hash recorded when w (its real folder
// and id) was trusted, and whether it is trusted at all. Only those exact
// bytes may run: a file that changed since, or one added to a workspace
// trusted without one, needs rex ws trust again.
func (s *TrustStore) TrustedHash(w *Workspace) (string, bool) {
	if w == nil || !ValidID(w.ID) {
		return "", false
	}
	root, err := realRoot(w.Root)
	if err != nil {
		return "", false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tf, err := s.read()
	if err != nil {
		return "", false
	}
	for _, e := range tf.Trusted {
		if e.Root == root && e.ID == w.ID {
			return e.SHA256, true
		}
	}
	return "", false
}

// Trust records w as trusted with the config.lua whose hash is sha (see
// ConfigHash; "" for none), replacing whatever its folder held before. A
// store file that cannot be read is an error and is left alone.
func (s *TrustStore) Trust(w *Workspace, sha string) error {
	if w == nil || !ValidID(w.ID) {
		return errors.New("workspace: trust needs a workspace with a valid id")
	}
	root, err := realRoot(w.Root)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	unlock, err := lockTrustFile(s.path)
	if err != nil {
		return err
	}
	defer unlock()
	tf, err := s.read()
	if err != nil {
		return err
	}
	tf.Trusted = slices.DeleteFunc(tf.Trusted, func(e TrustEntry) bool { return e.Root == root })
	tf.Trusted = append(tf.Trusted, TrustEntry{ID: w.ID, Root: root, SHA256: sha, TrustedAt: time.Now().UTC()})
	return s.write(tf)
}

// Revoke removes every entry for id or for the folder root (either may be
// empty, not both). It reports whether anything was removed.
func (s *TrustStore) Revoke(id, root string) (bool, error) {
	if id == "" && root == "" {
		return false, errors.New("workspace: revoke needs an id or a folder")
	}
	roots := map[string]bool{}
	if root != "" {
		roots[filepath.Clean(root)] = true
		if r, err := realRoot(root); err == nil {
			roots[r] = true
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	unlock, err := lockTrustFile(s.path)
	if err != nil {
		return false, err
	}
	defer unlock()
	tf, err := s.read()
	if err != nil {
		return false, err
	}
	n := len(tf.Trusted)
	tf.Trusted = slices.DeleteFunc(tf.Trusted, func(e TrustEntry) bool {
		return id != "" && e.ID == id || roots[e.Root]
	})
	if len(tf.Trusted) == n {
		return false, nil
	}
	return true, s.write(tf)
}

// List returns the trusted workspaces.
func (s *TrustStore) List() ([]TrustEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tf, err := s.read()
	return tf.Trusted, err
}
