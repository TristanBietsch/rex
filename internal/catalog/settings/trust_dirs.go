package settings

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/tristanbietsch/rex/internal/runtime/paths"
)

// The auto_trust_dirs checks live here so the daemon, which ignores what
// fails them, and rex setup, which refuses it, cannot drift apart.

// SplitTrustDirs splits an auto_trust_dirs value on the OS list separator
// (":", or ";" on Windows). The separator is also legal inside a folder
// name, so "/work/a:b" would split into "/work/a" (too broad) and "b".
// Every non-blank piece must therefore be absolute or start with "~/";
// otherwise the whole value is refused rather than guessed at.
func SplitTrustDirs(value string) ([]string, error) {
	dirs := filepath.SplitList(value)
	for _, d := range dirs {
		d = strings.TrimSpace(d)
		if d == "" || filepath.IsAbs(d) || paths.IsHomeRelative(d) {
			continue
		}
		if d == "~" {
			return nil, homeFolderError(d)
		}
		return nil, fmt.Errorf("%q is not an absolute or ~/ path (separate folders with %q)",
			d, string(filepath.ListSeparator))
	}
	return dirs, nil
}

// TrustDir resolves one auto_trust_dirs entry to the folder it trusts,
// through symlinks. It refuses entries that would trust far more than one
// project: a blank or relative entry, one containing "..", a filesystem
// root and the home folder itself. While HOME is set but cannot be
// resolved (it does not exist), it refuses every entry.
func TrustDir(entry string) (string, error) {
	d := strings.TrimSpace(entry)
	if d == "" {
		return "", errors.New("no folder given")
	}
	// Checked before ~/ is expanded: expanding cleans the path, which would
	// hide the ".." from the check below.
	if hasDotDot(d) {
		return "", fmt.Errorf("%q contains \"..\"; write the folder without it", d)
	}
	expanded, err := paths.ExpandHome(d)
	if err != nil {
		return "", err
	}
	dir, ok := CanonicalDir(expanded)
	if !ok {
		return "", fmt.Errorf("%q is not an absolute or ~/ path", d)
	}
	if filepath.Dir(dir) == dir {
		return "", fmt.Errorf("%q is a filesystem root; pick a folder inside it", d)
	}
	h, err := CanonicalHome()
	switch {
	case errors.Is(err, paths.ErrNoHome):
		// Without a home folder no entry can be it; ~/ entries failed above.
	case err != nil:
		// A home that is set but cannot be resolved could hide that the
		// entry is the home folder: refuse rather than guess.
		return "", fmt.Errorf("cannot check %q against the home folder: %w", d, err)
	case h == dir:
		return "", homeFolderError(d)
	}
	return dir, nil
}

// CanonicalHome returns the home folder resolved through symlinks, or an
// error wrapping paths.ErrNoHome when it is unknown. HOME is cleaned before
// it is resolved, as ExpandHome cleans "~/" entries: with HOME=/x/../home,
// "~/" expands to /home, so /home is the folder to compare against. HOME
// is the user's environment, not trust input, so its ".." is not refused.
func CanonicalHome() (string, error) {
	home, err := paths.Home()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(filepath.Clean(home))
}

// CheckAutoTrustDirs returns value tidied (pieces trimmed, blank ones
// dropped), or an error naming the first piece the daemon would ignore.
// rex setup uses it to refuse such a value instead of writing it.
func CheckAutoTrustDirs(value string) (string, error) {
	dirs, err := SplitTrustDirs(value)
	if err != nil {
		return "", err
	}
	var keep []string
	for _, d := range dirs {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		if _, err := TrustDir(d); err != nil {
			return "", err
		}
		keep = append(keep, d)
	}
	if len(keep) == 0 {
		return "", errors.New("no folder given")
	}
	return strings.Join(keep, string(filepath.ListSeparator)), nil
}

// normalizeAutoTrustDirs is the Normalize func of auto_trust_dirs: empty
// (always ask) stays empty, anything else must pass CheckAutoTrustDirs.
func normalizeAutoTrustDirs(v string) (string, error) {
	if v == "" {
		return "", nil
	}
	return CheckAutoTrustDirs(v)
}

// CanonicalDir resolves an absolute path through symlinks in its deepest
// existing ancestor, so a folder not created yet still compares equal to
// its resolved parent (on macOS /var is a link to /private/var).
//
// A path with a ".." element is refused. The kernel applies ".." after
// following symlinks (<trusted>/link/../x lands next to link's target),
// while cleaning the path first would apply it before; refusing keeps the
// check from approving a folder the agent would not actually start in.
func CanonicalDir(p string) (string, bool) {
	if !filepath.IsAbs(p) || hasDotDot(p) {
		return "", false
	}
	p = filepath.Clean(p)
	rest := ""
	for cur := p; ; {
		if r, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Join(r, rest), true
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return p, true
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}

func hasDotDot(p string) bool {
	for _, part := range strings.Split(filepath.ToSlash(p), "/") {
		if part == ".." {
			return true
		}
	}
	return false
}

func homeFolderError(d string) error {
	return fmt.Errorf("%q is your home folder; pick a folder inside it, such as ~/dev", d)
}
