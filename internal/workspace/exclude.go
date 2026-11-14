package workspace

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// maxExcludeBytes caps the exclude file rex will read and append to.
const maxExcludeBytes = 1 << 20

// ExcludeInGit adds the workspace's .rex/ to the repository's private ignore
// list (.git/info/exclude, never a tracked .gitignore) so it is not
// committed by accident, plus its audit.jsonl on a second line so an
// un-ignored .rex/ still never commits audits. root is the workspace folder;
// the repository is the nearest ancestor (root included) holding a .git
// directory or a .git file (a linked worktree).
//
// A repository is hostile input, so the write is fenced in:
//   - a .git file must name a git directory that points back at it (the
//     "gitdir" file git writes), and that directory must hold a HEAD file;
//   - a "commondir" must be an ancestor of the git directory holding a HEAD
//     file (the .../worktrees/<name> layout);
//   - info/ and info/exclude must not be symlinks, info/ must stay under the
//     common git directory, and the file is opened without following a link
//     and must be a regular file you own.
//
// Anything else returns an error wrapping ErrUnsafe and writes nothing. It
// reports whether a line was added: false when root is not in a repository
// or both lines are already there.
func ExcludeInGit(root string) (bool, error) {
	root = filepath.Clean(root)
	repo, gitDir, err := findGitDir(root)
	if err != nil {
		return false, err
	}
	if repo == "" {
		return false, nil
	}
	common, err := commonDir(gitDir)
	if err != nil {
		return false, err
	}
	rel, err := filepath.Rel(repo, root)
	if err != nil {
		return false, err
	}
	prefix := "/"
	if rel != "." {
		prefix = "/" + filepath.ToSlash(rel) + "/"
	}
	want := []string{prefix + DirName + "/", prefix + DirName + "/" + AuditFileName}

	infoDir := filepath.Join(common, "info")
	if err := ensureInfoDir(common, infoDir); err != nil {
		return false, err
	}
	f, err := os.OpenFile(filepath.Join(infoDir, "exclude"), os.O_RDWR|os.O_APPEND|os.O_CREATE|oNoFollow|oNonBlock, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || !isLinkOpenErr(err) {
			return false, err
		}
		return false, fmt.Errorf("%w: %s is a symlink", ErrUnsafe, filepath.Join(infoDir, "exclude"))
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return false, err
	}
	if !fi.Mode().IsRegular() || !ownedByCurrentUser(fi) {
		return false, fmt.Errorf("%w: %s is not a regular file you own", ErrUnsafe, f.Name())
	}
	if fi.Size() > maxExcludeBytes {
		return false, fmt.Errorf("%w: %s is larger than %d bytes", ErrUnsafe, f.Name(), maxExcludeBytes)
	}
	existing, err := io.ReadAll(io.NewSectionReader(f, 0, fi.Size()))
	if err != nil {
		return false, err
	}
	have := map[string]bool{}
	for _, l := range bytes.Split(existing, []byte("\n")) {
		have[strings.TrimSpace(string(l))] = true
	}
	var add strings.Builder
	if len(existing) > 0 && !bytes.HasSuffix(existing, []byte("\n")) {
		add.WriteString("\n")
	}
	added := false
	for _, w := range want {
		if !have[w] {
			add.WriteString(w + "\n")
			added = true
		}
	}
	if !added {
		return false, nil
	}
	if _, err := f.WriteString(add.String()); err != nil {
		return false, err
	}
	return true, f.Close()
}

// ensureInfoDir makes common/info a real directory under common (0700 when
// created). A symlink, a non-directory, or a path that resolves outside the
// common git directory is refused.
func ensureInfoDir(common, infoDir string) error {
	fi, err := os.Lstat(infoDir)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if err := os.Mkdir(infoDir, 0o700); err != nil {
			return err
		}
	case err != nil:
		return err
	case fi.Mode()&os.ModeSymlink != 0 || !fi.IsDir():
		return fmt.Errorf("%w: %s is a symlink or not a directory", ErrUnsafe, infoDir)
	}
	rc, err := filepath.EvalSymlinks(common)
	if err != nil {
		return err
	}
	ri, err := filepath.EvalSymlinks(infoDir)
	if err != nil {
		return err
	}
	if !within(rc, ri) {
		return fmt.Errorf("%w: %s resolves outside %s", ErrUnsafe, infoDir, common)
	}
	return nil
}

// within reports whether child is strictly below parent.
func within(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// findGitDir walks up from dir to the first folder with a .git entry and
// returns that folder and its git directory ("" "" when none).
func findGitDir(dir string) (repo, gitDir string, err error) {
	for {
		g := filepath.Join(dir, ".git")
		fi, serr := os.Lstat(g)
		switch {
		case serr == nil && fi.IsDir():
			return dir, g, nil
		case serr == nil && fi.Mode().IsRegular():
			gd, perr := parseGitFile(g)
			if perr != nil {
				return "", "", perr
			}
			return dir, gd, nil
		case serr != nil && !errors.Is(serr, os.ErrNotExist) && !isNotDirErr(serr):
			return "", "", serr
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", "", nil
		}
		dir = parent
	}
}

// maxGitFileBytes bounds a ".git" file ("gitdir: <path>") and a commondir
// or gitdir link file.
const maxGitFileBytes = 4 << 10

// readSmall reads a small link file and returns its trimmed text.
func readSmall(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxGitFileBytes+1))
	if err != nil {
		return "", err
	}
	if len(b) > maxGitFileBytes {
		return "", fmt.Errorf("%w: %s is too large", ErrUnsafe, path)
	}
	return strings.TrimSpace(string(b)), nil
}

// resolve makes p absolute against base and resolves symlinks.
func resolve(p, base string) (string, error) {
	if !filepath.IsAbs(p) {
		p = filepath.Join(base, p)
	}
	return filepath.EvalSymlinks(filepath.Clean(p))
}

// parseGitFile reads a ".git" file and returns the git directory it names.
// The target must hold a HEAD file and a "gitdir" file naming this .git
// file (git writes both for a linked worktree), so a hostile repo cannot
// aim rex's write at an arbitrary folder.
func parseGitFile(path string) (string, error) {
	text, err := readSmall(path)
	if err != nil {
		return "", err
	}
	rest, ok := strings.CutPrefix(text, "gitdir:")
	if !ok {
		return "", fmt.Errorf("%w: %s is not a gitdir file", ErrUnsafe, path)
	}
	gd, err := resolve(strings.TrimSpace(rest), filepath.Dir(path))
	if err != nil {
		return "", fmt.Errorf("%w: %s: %w", ErrUnsafe, path, err)
	}
	if fi, err := os.Stat(filepath.Join(gd, "HEAD")); err != nil || !fi.Mode().IsRegular() {
		return "", fmt.Errorf("%w: %s: %s is not a git directory", ErrUnsafe, path, gd)
	}
	back, err := readSmall(filepath.Join(gd, "gitdir"))
	if err != nil {
		return "", fmt.Errorf("%w: %s: %s does not point back at it", ErrUnsafe, path, gd)
	}
	backAbs, err := resolve(back, gd)
	if err != nil {
		return "", fmt.Errorf("%w: %s: back-link: %w", ErrUnsafe, path, err)
	}
	self, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	if backAbs != self {
		return "", fmt.Errorf("%w: %s: %s points back at %s", ErrUnsafe, path, gd, backAbs)
	}
	return gd, nil
}

// commonDir is where info/exclude lives: a linked worktree's git dir names
// the shared one in its "commondir" file, which must be an ancestor of the
// git directory (.../worktrees/<name>) holding a HEAD file; otherwise it is
// gitDir itself.
func commonDir(gitDir string) (string, error) {
	text, err := readSmall(filepath.Join(gitDir, "commondir"))
	if errors.Is(err, os.ErrNotExist) || (err == nil && text == "") {
		return gitDir, nil
	}
	if err != nil {
		return "", err
	}
	c, err := resolve(text, gitDir)
	if err != nil {
		return "", fmt.Errorf("%w: commondir %q: %w", ErrUnsafe, text, err)
	}
	rg, err := filepath.EvalSymlinks(gitDir)
	if err != nil {
		return "", err
	}
	if !within(c, rg) {
		return "", fmt.Errorf("%w: commondir %s is not an ancestor of %s", ErrUnsafe, c, gitDir)
	}
	if fi, err := os.Stat(filepath.Join(c, "HEAD")); err != nil || !fi.Mode().IsRegular() {
		return "", fmt.Errorf("%w: commondir %s is not a git directory", ErrUnsafe, c)
	}
	return c, nil
}
