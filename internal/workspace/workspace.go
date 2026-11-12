// Package workspace identifies project folders rex was told to treat as one
// unit. A workspace is a folder holding a .rex/ directory with a
// workspace.json (id, name, color). The id is what ties sessions and audit
// records to the folder; the folder itself may move.
//
// Everything here is inert data: nothing in a .rex/ can change rex's
// behavior. A repo cloned from the internet may ship a .rex/, so discovery
// treats it as hostile input: it never follows a symlink, never reads a file
// over maxFileBytes, and refuses a .rex/ that another user owns or that is
// group- or world-writable (ErrUnsafe).
package workspace

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// File and directory names inside a workspace root.
const (
	// DirName is the workspace marker directory.
	DirName = ".rex"
	// FileName is the identity file inside DirName.
	FileName = "workspace.json"
	// AuditFileName is the workspace's own audit trail inside DirName.
	AuditFileName = "audit.jsonl"

	// MaxNameRunes caps a workspace's display name.
	MaxNameRunes = 40
	// maxFileBytes caps workspace.json; a larger file is rejected unread.
	maxFileBytes = 64 << 10
)

// ErrUnsafe reports a .rex/ rex refuses to use: a symlink, a directory that
// is group- or world-writable or owned by another user, or an identity file
// that is not a plain private file.
var ErrUnsafe = errors.New("workspace: unsafe .rex")

// ErrInvalid reports a workspace.json that cannot be used: too large, not
// JSON, or with a malformed id.
var ErrInvalid = errors.New("workspace: invalid workspace.json")

var (
	idPattern    = regexp.MustCompile(`^[0-9a-f]{8}$`)
	colorPattern = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)
)

// Workspace is one workspace's identity.
type Workspace struct {
	ID        string    `json:"id"`    // 8 lowercase hex
	Name      string    `json:"name"`  // at most MaxNameRunes runes, display only
	Color     string    `json:"color"` // "#RRGGBB", normally one of Swatches
	CreatedAt time.Time `json:"created_at"`
	Root      string    `json:"-"` // folder that holds .rex/
}

// ValidID reports whether id is 8 lowercase hex digits.
func ValidID(id string) bool { return idPattern.MatchString(id) }

// ValidColor reports whether c is "#RRGGBB".
func ValidColor(c string) bool { return colorPattern.MatchString(c) }

// CleanName strips control and line-break runes from a display name, trims
// it and keeps at most MaxNameRunes runes.
func CleanName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == utf8.RuneError, unicode.IsControl(r), unicode.Is(unicode.Cf, r),
			r == ' ', r == ' ':
			continue
		}
		b.WriteRune(r)
	}
	out := strings.TrimSpace(b.String())
	if utf8.RuneCountInString(out) > MaxNameRunes {
		out = strings.TrimSpace(string([]rune(out)[:MaxNameRunes]))
	}
	return out
}

// Find returns the workspace around cwd: the nearest ancestor (cwd itself
// included) that holds a .rex/ with a workspace.json. The walk stops before
// home and before the filesystem root, so neither can be a workspace. It
// returns nil, nil when there is none, and an error wrapping ErrUnsafe or
// ErrInvalid when the nearest .rex/ cannot be used (the walk does not skip
// past it to an outer one).
func Find(cwd, home string) (*Workspace, error) {
	if !filepath.IsAbs(cwd) {
		return nil, fmt.Errorf("workspace: cwd %q is not absolute", cwd)
	}
	dir := filepath.Clean(cwd)
	home = filepath.Clean(home)
	for {
		parent := filepath.Dir(dir)
		if parent == dir || dir == home { // filesystem root, or home
			return nil, nil
		}
		w, err := load(dir)
		if err != nil || w != nil {
			return w, err
		}
		dir = parent
	}
}

// Load reads the workspace whose root is exactly root (no walk upward). It
// returns nil, nil when root has no workspace.json.
func Load(root string) (*Workspace, error) {
	if !filepath.IsAbs(root) {
		return nil, fmt.Errorf("workspace: root %q is not absolute", root)
	}
	return load(filepath.Clean(root))
}

// load reads root/.rex/workspace.json. nil, nil when root has no .rex/ or
// no workspace.json (a .rex/ without an identity is not a workspace).
func load(root string) (*Workspace, error) {
	rexDir := filepath.Join(root, DirName)
	if err := checkDir(rexDir); err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, errNotDir) {
			return nil, nil
		}
		return nil, err
	}
	path := filepath.Join(rexDir, FileName)
	fi, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: %s is not a regular file", ErrUnsafe, path)
	}
	if fi.Mode().Perm()&0o022 != 0 && !skipModeChecks {
		return nil, fmt.Errorf("%w: %s is group- or world-writable", ErrUnsafe, path)
	}
	if fi.Size() > maxFileBytes {
		return nil, fmt.Errorf("%w: %s is larger than %d bytes", ErrInvalid, path, maxFileBytes)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxFileBytes {
		return nil, fmt.Errorf("%w: %s is larger than %d bytes", ErrInvalid, path, maxFileBytes)
	}
	var w Workspace
	if err := json.Unmarshal(b, &w); err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrInvalid, path, err)
	}
	if !ValidID(w.ID) {
		return nil, fmt.Errorf("%w: %s: id must be 8 lowercase hex digits", ErrInvalid, path)
	}
	w.Name = CleanName(w.Name)
	if !ValidColor(w.Color) {
		w.Color = ""
	}
	w.Root = root
	return &w, nil
}

var errNotDir = errors.New("not a directory")

// checkDir validates root/.rex: a real directory, owned by the current user
// and not writable by group or others. A missing one wraps os.ErrNotExist; a
// regular file of that name wraps errNotDir.
func checkDir(rexDir string) error {
	fi, err := os.Lstat(rexDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || isNotDirErr(err) {
			return os.ErrNotExist
		}
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: %s is a symlink", ErrUnsafe, rexDir)
	}
	if !fi.IsDir() {
		return errNotDir
	}
	if skipModeChecks {
		return nil
	}
	if fi.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("%w: %s is group- or world-writable", ErrUnsafe, rexDir)
	}
	if !ownedByCurrentUser(fi) {
		return fmt.Errorf("%w: %s is owned by another user", ErrUnsafe, rexDir)
	}
	return nil
}

// Save writes w's identity to w.Root/.rex/workspace.json (0600, atomically).
// The .rex/ is created 0700 when missing and checked as Find does.
func Save(w *Workspace) error {
	if !ValidID(w.ID) {
		return fmt.Errorf("workspace: bad id %q", w.ID)
	}
	if w.Root == "" || !filepath.IsAbs(w.Root) {
		return fmt.Errorf("workspace: root %q is not absolute", w.Root)
	}
	rexDir := filepath.Join(w.Root, DirName)
	switch err := checkDir(rexDir); {
	case errors.Is(err, os.ErrNotExist):
		if err := os.Mkdir(rexDir, 0o700); err != nil {
			return err
		}
		if err := os.Chmod(rexDir, 0o700); err != nil { //nolint:gosec // a directory: owner rwx is the point
			return err
		}
	case err != nil:
		return err
	}
	out := *w
	out.Name = CleanName(out.Name)
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(rexDir, FileName), append(b, '\n'))
}

// writeFileAtomic writes b to path through a 0600 temp file in the same
// directory and a rename. An existing symlink or non-regular file at path is
// refused, not followed.
func writeFileAtomic(path string, b []byte) error {
	if fi, err := os.Lstat(path); err == nil && !fi.Mode().IsRegular() {
		return fmt.Errorf("%w: %s is not a regular file", ErrUnsafe, path)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	_, werr := tmp.Write(b)
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Chmod(name, 0o600); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
		return err
	}
	return nil
}
