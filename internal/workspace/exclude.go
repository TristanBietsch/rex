package workspace

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ExcludeInGit adds the workspace's .rex/ to the repository's private ignore
// list (.git/info/exclude, never a tracked .gitignore) so it is not
// committed by accident. root is the workspace folder; the repository is the
// nearest ancestor (root included) holding a .git directory or a .git file
// (a worktree or submodule pointing at its git dir).
//
// It reports whether a line was added: false when root is not in a
// repository or the line is already there. Calling it again changes nothing.
func ExcludeInGit(root string) (bool, error) {
	root = filepath.Clean(root)
	repo, gitDir, err := findGitDir(root)
	if err != nil {
		return false, err
	}
	if repo == "" {
		return false, nil
	}
	rel, err := filepath.Rel(repo, root)
	if err != nil {
		return false, err
	}
	line := "/" + DirName + "/"
	if rel != "." {
		line = "/" + filepath.ToSlash(rel) + "/" + DirName + "/"
	}

	infoDir := filepath.Join(commonDir(gitDir), "info")
	exclude := filepath.Join(infoDir, "exclude")
	if fi, err := os.Lstat(exclude); err == nil && !fi.Mode().IsRegular() {
		return false, fmt.Errorf("%w: %s is not a regular file", ErrUnsafe, exclude)
	}
	existing, err := os.ReadFile(exclude)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	sc := bufio.NewScanner(bytes.NewReader(existing))
	for sc.Scan() {
		if strings.TrimSpace(sc.Text()) == line {
			return false, nil
		}
	}
	if err := os.MkdirAll(infoDir, 0o750); err != nil {
		return false, err
	}
	f, err := os.OpenFile(exclude, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644) //nolint:gosec // git's own file, git's own mode
	if err != nil {
		return false, err
	}
	add := line + "\n"
	if len(existing) > 0 && !bytes.HasSuffix(existing, []byte("\n")) {
		add = "\n" + add
	}
	_, werr := f.WriteString(add)
	if err := errors.Join(werr, f.Close()); err != nil {
		return false, err
	}
	return true, nil
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

// maxGitFileBytes bounds a ".git" file ("gitdir: <path>").
const maxGitFileBytes = 4 << 10

// parseGitFile reads a ".git" file and returns the git directory it names.
// The target must look like a git directory (it holds a HEAD file), so a
// hostile repo cannot aim rex's write at an arbitrary folder.
func parseGitFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if len(b) > maxGitFileBytes {
		return "", fmt.Errorf("workspace: %s is too large", path)
	}
	rest, ok := strings.CutPrefix(strings.TrimSpace(string(b)), "gitdir:")
	if !ok {
		return "", fmt.Errorf("workspace: %s is not a gitdir file", path)
	}
	gd := strings.TrimSpace(rest)
	if !filepath.IsAbs(gd) {
		gd = filepath.Join(filepath.Dir(path), gd)
	}
	//nolint:gosec // gd comes from the repo's own .git file; only a stat, and the target must hold a HEAD file
	if fi, err := os.Stat(filepath.Join(gd, "HEAD")); err != nil || !fi.Mode().IsRegular() {
		return "", fmt.Errorf("workspace: %s: %s is not a git directory", path, gd)
	}
	return filepath.Clean(gd), nil
}

// commonDir is where info/exclude lives: a linked worktree's git dir names
// the shared one in its "commondir" file; otherwise it is gitDir itself.
func commonDir(gitDir string) string {
	b, err := os.ReadFile(filepath.Join(gitDir, "commondir"))
	if err != nil || len(b) > maxGitFileBytes {
		return gitDir
	}
	c := strings.TrimSpace(string(b))
	if c == "" {
		return gitDir
	}
	if !filepath.IsAbs(c) {
		c = filepath.Join(gitDir, c)
	}
	return filepath.Clean(c)
}
