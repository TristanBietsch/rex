package workspace

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/tristanbietsch/rex/internal/runtime/paths"
)

// ErrNotFound reports a reference that names no workspace: an id the
// registry does not know, or a folder with no workspace around it.
var ErrNotFound = errors.New("workspace: not found")

// ErrMoved reports a registered workspace whose folder no longer holds it.
var ErrMoved = errors.New("workspace: not at its registered folder")

// Resolve finds the workspace a user named: ref is an 8-hex id (looked up in
// reg) or a folder, absolute, "~/..." or relative to cwd ("" is cwd itself),
// meaning the workspace around it. The error wraps ErrNotFound or ErrMoved
// for a miss, and ErrUnsafe or ErrInvalid for a .rex/ that cannot be used.
func Resolve(ref, cwd, home string, reg *Registry) (*Workspace, error) {
	if ValidID(ref) {
		if reg == nil {
			return nil, fmt.Errorf("%w: no registry to look up id %s", ErrNotFound, ref)
		}
		en, ok := reg.Get(ref)
		if !ok {
			return nil, fmt.Errorf("%w: no workspace with id %s", ErrNotFound, ref)
		}
		w, err := Load(en.Root)
		if err != nil {
			return nil, err
		}
		if w == nil || w.ID != ref {
			return nil, fmt.Errorf("%w: %s is not at %s any more", ErrMoved, ref, en.Root)
		}
		return w, nil
	}
	dir, err := AbsPath(ref, cwd)
	if err != nil {
		return nil, err
	}
	w, err := Find(dir, home)
	if err != nil {
		return nil, err
	}
	if w == nil {
		return nil, fmt.Errorf("%w: no .rex/ at or above %s", ErrNotFound, dir)
	}
	return w, nil
}

// AbsPath makes p absolute: "" is cwd, "~/x" is under the home directory, a
// relative path is under cwd. Symlinks are not resolved.
func AbsPath(p, cwd string) (string, error) {
	switch {
	case p == "":
		return cwd, nil
	case paths.IsHomeRelative(p):
		return paths.ExpandHome(p)
	case filepath.IsAbs(p):
		return filepath.Clean(p), nil
	}
	return filepath.Join(cwd, p), nil
}
