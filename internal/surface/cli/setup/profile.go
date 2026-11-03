package setup

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// Shell profile handling for `rex uninstall -strip-profile`.
//
// `rex setup` and `install.sh --shell-init` wrap everything they add to a
// shell profile in "# BEGIN REX" / "# END REX" marker lines. Uninstall
// removes those blocks. A shell profile is irreplaceable user data, so the
// rewrite follows three rules:
//
//   - Unbalanced markers abort the file. Nothing is written and the user is
//     told which line to fix, because guessing where a block ends can
//     delete the rest of the profile (P0-09).
//   - The original is copied to a timestamped backup before it is
//     replaced, and the backup is never overwritten.
//   - Symlinked profiles (stow, chezmoi, a dotfiles checkout) are rewritten
//     at their target, so the link survives, and the file keeps its mode.

// Marker lines, compared case-insensitively after trimming surrounding
// whitespace (which also drops a CRLF's '\r').
const (
	profileBlockBegin = "# BEGIN REX"
	profileBlockEnd   = "# END REX"
)

// Errors for unbalanced markers. stripBlockFromFile wraps them with the
// offending line number.
var (
	errUnterminatedBlock = errors.New(profileBlockBegin + " has no matching " + profileBlockEnd)
	errNestedBlock       = errors.New(profileBlockBegin + " inside another REX block")
	errUnmatchedEnd      = errors.New(profileBlockEnd + " without a preceding " + profileBlockBegin)
)

// profileFiles lists the shell profiles uninstall scans. It must cover
// every file `rex setup` can write the block into (see detectShellRC in
// internal/surface/tui/setup_wizard.go: .zshrc, .bashrc, config.fish, and
// .profile as the fallback) plus .bash_profile, a common target for
// `install.sh --shell-init >>`.
func profileFiles(home string) []string {
	return []string{
		filepath.Join(home, ".bashrc"),
		filepath.Join(home, ".bash_profile"),
		filepath.Join(home, ".zshrc"),
		filepath.Join(home, ".profile"),
		filepath.Join(home, ".config", "fish", "config.fish"),
	}
}

// profileMarker classifies one profile line.
type profileMarker int

const (
	markerNone profileMarker = iota
	markerBegin
	markerEnd
)

// markerOf reports whether line (with or without its line ending) is a
// BEGIN or END REX marker. Only a whole line counts, so a marker quoted
// inside a command is ordinary text.
func markerOf(line []byte) profileMarker {
	trimmed := bytes.TrimSpace(line)
	switch {
	case bytes.EqualFold(trimmed, []byte(profileBlockBegin)):
		return markerBegin
	case bytes.EqualFold(trimmed, []byte(profileBlockEnd)):
		return markerEnd
	default:
		return markerNone
	}
}

// removeProfileBlocks returns data without its BEGIN … END REX blocks
// (marker lines included). Every line outside a block is copied byte for
// byte, so CRLF endings and a missing final newline survive, and line
// length is unbounded. found reports whether any block was removed.
//
// Unbalanced markers return an error and no output: an unterminated BEGIN,
// a BEGIN inside a block, or an END outside one.
func removeProfileBlocks(data []byte) (out []byte, found bool, err error) {
	var buf bytes.Buffer
	buf.Grow(len(data))
	openedAt := 0 // 1-based line of the open BEGIN; 0 outside a block
	n := 0
	for line := range bytes.Lines(data) {
		n++
		switch markerOf(line) {
		case markerBegin:
			if openedAt != 0 {
				return nil, false, fmt.Errorf("line %d: %w (opened on line %d)", n, errNestedBlock, openedAt)
			}
			openedAt = n
			found = true
		case markerEnd:
			if openedAt == 0 {
				return nil, false, fmt.Errorf("line %d: %w", n, errUnmatchedEnd)
			}
			openedAt = 0
		case markerNone:
			if openedAt == 0 {
				buf.Write(line)
			}
		}
	}
	if openedAt != 0 {
		return nil, false, fmt.Errorf("line %d: %w", openedAt, errUnterminatedBlock)
	}
	return buf.Bytes(), found, nil
}

// hasProfileBlock reports whether the file at path has a BEGIN REX marker
// line. It uses the same line matching as the stripper, so the preview
// lists exactly the files uninstall would try to rewrite.
func hasProfileBlock(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	for line := range bytes.Lines(data) {
		if markerOf(line) == markerBegin {
			return true
		}
	}
	return false
}

// profileFilesWithBlock returns only the files that contain a BEGIN REX block.
func profileFilesWithBlock(files []string) []string {
	var found []string
	for _, f := range files {
		if hasProfileBlock(f) {
			found = append(found, f)
		}
	}
	return found
}

// strippedProfile records one rewritten profile and where its original went.
type strippedProfile struct {
	Path   string // the profile as listed (may be a symlink)
	Backup string // copy of the original content, next to Path
}

// stripProfileBlocks strips REX blocks from each profile in paths. A
// failure on one file is collected and does not stop the others; that file
// is left as it was. stamp names the backups.
func stripProfileBlocks(paths []string, stamp time.Time) ([]strippedProfile, []error) {
	var stripped []strippedProfile
	var errs []error
	for _, p := range paths {
		backup, err := stripBlockFromFile(p, stamp)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", p, err))
			continue
		}
		if backup != "" {
			stripped = append(stripped, strippedProfile{Path: p, Backup: backup})
			slog.Info("uninstall: stripped profile block", "file", p, "backup", backup)
		}
	}
	return stripped, errs
}

// stripBlockFromFile removes the REX blocks from the profile at path and
// returns the backup it wrote, or "" when there was nothing to remove (or
// no file). A missing file or dangling symlink is not an error.
//
// On any error the profile is unchanged: the content is validated before
// the backup is written, and the rewrite is an atomic rename of a temp
// file in the target's directory.
func stripBlockFromFile(path string, stamp time.Time) (backup string, err error) {
	target, err := filepath.EvalSymlinks(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("resolve symlinks: %w", err)
	}
	info, err := os.Stat(target)
	if err != nil {
		return "", fmt.Errorf("stat %s: %w", target, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a regular file; left unchanged", target)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", target, err)
	}
	out, found, err := removeProfileBlocks(data)
	if err != nil {
		return "", fmt.Errorf("%w; fix the markers by hand, the file was left unchanged", err)
	}
	if !found {
		return "", nil
	}

	perm := info.Mode().Perm()
	backup = backupPath(path, stamp)
	if err := writeNewFile(backup, data, perm); err != nil {
		return "", fmt.Errorf("back up before rewriting (file left unchanged): %w", err)
	}
	if err := replaceFile(target, out, perm); err != nil {
		return "", fmt.Errorf("rewrite %s (original kept, backup at %s): %w", target, backup, err)
	}
	return backup, nil
}

// backupPath names the backup after the profile path the user knows (not a
// symlink's target, which may sit in a dotfiles checkout), e.g.
// ~/.zshrc.rex-backup-1700000000.
func backupPath(path string, stamp time.Time) string {
	return path + ".rex-backup-" + strconv.FormatInt(stamp.Unix(), 10)
}

// writeNewFile writes data to a file that must not exist yet, so an earlier
// backup is never clobbered. The file is synced and gets mode perm exactly.
func writeNewFile(path string, data []byte, perm fs.FileMode) (err error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = os.Remove(path)
		}
	}()
	if err = writeSyncClose(f, data); err != nil {
		return err
	}
	// OpenFile's mode is masked by the umask; match the original exactly.
	return os.Chmod(path, perm)
}

// replaceFile atomically replaces the regular file at path with data and
// mode perm: it writes a synced temp file in the same directory and renames
// it over path. path must already be symlink-resolved, or the rename would
// replace the link instead of the file it points to.
func replaceFile(path string, data []byte, perm fs.FileMode) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".rex-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(tmpName)
		}
	}()
	if err = writeSyncClose(tmp, data); err != nil {
		return err
	}
	if err = os.Chmod(tmpName, perm); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// writeSyncClose writes data to f, flushes it to stable storage and closes
// f. f is closed on every path.
func writeSyncClose(f *os.File, data []byte) error {
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
