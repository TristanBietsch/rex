package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/tristanbietsch/rex/internal/runtime/paths"
)

// ErrRefusedRoot reports a folder that cannot be a workspace: the home
// directory or the filesystem root. Either would make every session on the
// machine part of one workspace.
var ErrRefusedRoot = errors.New("workspace: refusing to use the home directory or the filesystem root")

// CheckRoot returns nil when root is an existing folder that may become a
// workspace, else an error (wrapping ErrRefusedRoot for $HOME and "/").
func CheckRoot(root string) error {
	if !filepath.IsAbs(root) {
		return fmt.Errorf("workspace: %q is not an absolute path", root)
	}
	root = filepath.Clean(root)
	fi, err := os.Stat(root)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return fmt.Errorf("workspace: %s is not a folder", root)
	}
	if filepath.Dir(root) == root {
		return ErrRefusedRoot
	}
	home, err := paths.Home()
	if err != nil {
		return err
	}
	if sameDir(root, home) {
		return ErrRefusedRoot
	}
	return nil
}

// sameDir reports whether a and b are the same existing folder (symlinks
// and case folding resolved by the file system).
func sameDir(a, b string) bool {
	fa, err := os.Stat(a)
	if err != nil {
		return false
	}
	fb, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(fa, fb)
}

// Init makes root a workspace: .rex/ (0700) with a workspace.json (0600)
// holding a fresh id, name and color, and registers it in reg (nil: no
// registry). A root that already is a workspace is returned as it is (its
// name is kept; a non-empty name renames it). The home directory and "/" are
// refused (ErrRefusedRoot). name defaults to the folder's name.
func Init(root, name string, reg *Registry) (*Workspace, error) {
	if err := CheckRoot(root); err != nil {
		return nil, err
	}
	root = filepath.Clean(root)
	existing, err := load(root)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		if name != "" && CleanName(name) != existing.Name {
			existing.Name = CleanName(name)
			if err := Save(existing); err != nil {
				return nil, err
			}
		}
		if reg != nil {
			if err := reg.Seen(existing); err != nil {
				return nil, err
			}
		}
		return existing, nil
	}

	if name == "" {
		name = filepath.Base(root)
	}
	var taken func(string) bool
	var used []string
	if reg != nil {
		taken = reg.Taken
		used = reg.usedColors()
	}
	id, err := newID(taken)
	if err != nil {
		return nil, err
	}
	w := &Workspace{
		ID:        id,
		Name:      CleanName(name),
		Color:     pickColor(used),
		CreatedAt: time.Now().UTC().Truncate(time.Second),
		Root:      root,
	}
	if err := Save(w); err != nil {
		return nil, err
	}
	if reg != nil {
		if err := reg.Seen(w); err != nil {
			return nil, err
		}
	}
	return w, nil
}
