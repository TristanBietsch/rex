package settings

import (
	"errors"
	"fmt"
	"os"
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
	case h == dir || sameFolder(dir, h):
		return "", homeFolderError(d)
	case holdsFolder(dir, h):
		return "", fmt.Errorf("%q holds your home folder; pick a folder inside it, such as ~/dev", d)
	}
	return dir, nil
}

// holdsFolder reports whether dir is a proper ancestor of home h. Whole path
// elements are compared first (so /Users2 does not hold /Users/me); then each
// parent of h is compared by identity on disk, which catches a case variant
// on a case-insensitive filesystem.
func holdsFolder(dir, h string) bool {
	if strings.HasPrefix(h, strings.TrimSuffix(dir, string(filepath.Separator))+string(filepath.Separator)) {
		return true
	}
	for p := filepath.Dir(h); ; p = filepath.Dir(p) {
		if sameFolder(dir, p) {
			return true
		}
		if filepath.Dir(p) == p {
			return false
		}
	}
}

// sameFolder reports whether a and b are the same folder on disk. The path
// strings of a case variant differ on a case-insensitive filesystem (macOS,
// Windows) though they name one folder.
func sameFolder(a, b string) bool {
	ai, err := os.Stat(a)
	if err != nil {
		return false
	}
	bi, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(ai, bi)
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

// SuggestTrustDir offers the folder to trust for a session started in cwd:
// the root of the git checkout holding cwd, or cwd itself when it is not in
// one. It never offers the parent of a project. The suggestion is the
// "~/..." form of the resolved folder and must pass the checks a typed value
// does and lie strictly inside the resolved home folder, so it is never the
// home folder, a filesystem root or anything outside home (such as /tmp).
// The search for a checkout stops below home, so a repo in home (dotfiles)
// does not turn every project into the home folder. It returns "" when
// nothing fits.
func SuggestTrustDir(cwd string) string {
	dir, ok := CanonicalDir(cwd)
	if !ok {
		return ""
	}
	home, err := CanonicalHome()
	if err != nil {
		return ""
	}
	prefix := home + string(filepath.Separator)
	if !strings.HasPrefix(dir, prefix) {
		return ""
	}
	for cur := dir; strings.HasPrefix(cur, prefix); cur = filepath.Dir(cur) {
		if _, err := os.Stat(filepath.Join(cur, ".git")); err == nil {
			dir = cur
			break
		}
	}
	short := "~" + string(filepath.Separator) + strings.TrimPrefix(dir, prefix)
	// A name holding the list separator would be read back as two folders,
	// and one with outer spaces would be trimmed into another folder.
	if strings.ContainsRune(short, filepath.ListSeparator) {
		return ""
	}
	if v, err := CheckAutoTrustDirs(short); err != nil || v != short {
		return ""
	}
	return short
}

// AppendTrustDir adds dir to the auto_trust_dirs value cur. It reports false
// and returns cur tidied when dir is already covered (it is, or lies inside,
// a listed folder). dir must pass TrustDir and the whole new value
// CheckAutoTrustDirs, so a refused folder or a cur the daemon would ignore
// is an error, never carried along.
func AppendTrustDir(cur, dir string) (string, bool, error) {
	entry := strings.TrimSpace(dir)
	if strings.ContainsRune(entry, filepath.ListSeparator) {
		return "", false, fmt.Errorf("%q holds %q, which separates folders; add one folder at a time",
			entry, string(filepath.ListSeparator))
	}
	want, err := TrustDir(entry)
	if err != nil {
		return "", false, err
	}
	if strings.TrimSpace(cur) != "" {
		tidy, err := CheckAutoTrustDirs(cur)
		if err != nil {
			return "", false, err
		}
		for _, e := range filepath.SplitList(tidy) {
			if got, err := TrustDir(e); err == nil && (got == want || strings.HasPrefix(want, got+string(filepath.Separator))) {
				return tidy, false, nil
			}
		}
		entry = tidy + string(filepath.ListSeparator) + entry
	}
	v, err := CheckAutoTrustDirs(entry)
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

// RemoveTrustDir drops from the auto_trust_dirs value cur every entry that
// resolves to dir (by identity on disk, so a case variant on a
// case-insensitive filesystem counts) or is written exactly as dir, and
// reports whether any did. The text match lets an entry the daemon ignores
// (rex doctor names it) be removed, and other ignored entries never block a
// removal. The result may be empty (always ask). A dir that matches no entry
// by text must pass TrustDir.
func RemoveTrustDir(cur, dir string) (string, bool, error) {
	entry := strings.TrimSpace(dir)
	want, wantErr := TrustDir(entry)
	var keep []string
	removed := false
	for _, e := range filepath.SplitList(cur) {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		if e == entry || (wantErr == nil && resolvesTo(e, want)) {
			removed = true
			continue
		}
		keep = append(keep, e)
	}
	if !removed && wantErr != nil {
		return "", false, wantErr
	}
	return strings.Join(keep, string(filepath.ListSeparator)), removed, nil
}

// resolvesTo reports whether entry passes TrustDir and names the folder want.
func resolvesTo(entry, want string) bool {
	got, err := TrustDir(entry)
	return err == nil && (got == want || sameFolder(got, want))
}
