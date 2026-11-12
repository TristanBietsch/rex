package spawnctx

import (
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Workspace paths (R16 "Path rules"): the import picker and @ completion
// add files and folders of the session folder by their relative path. A
// path is resolved against the folder and through every symlink, and the
// result must stay inside the resolved folder; only regular files and
// folders count (a FIFO, socket or device is refused from its Lstat, never
// opened); a name with control characters or line separators is refused
// (it would break the prompt's list); a name that looks like it holds
// secrets is flagged for a confirm, not refused (D9).

// BigFile is the size above which a file item carries a warning: the agent
// may not read all of it. rex itself never reads more than a preview.
const BigFile = 1 << 20

// The reasons a path is refused, wrapped in a *PathError.
var (
	ErrOutside = errors.New("outside the workspace")
	ErrSpecial = errors.New("not a regular file or folder")
	ErrBadName = errors.New("control characters or line separators in the name")
	ErrBinary  = errors.New("binary data, not text")
	ErrIsDir   = errors.New("a folder, not a file")
)

// PathError is a refused path: Path as it was asked for, and for a symlink
// that leads outside the workspace, the Target it resolves to.
type PathError struct {
	Path   string
	Target string
	Err    error
}

func (e *PathError) Error() string {
	name := quoteBad(e.Path)
	if e.Target != "" {
		return name + ": links to " + quoteBad(e.Target) + ", " + e.Err.Error()
	}
	return name + ": " + e.Err.Error()
}

// quoteBad quotes a path holding a rune badNameRune refuses, so an error
// never carries a raw newline or escape.
func quoteBad(s string) string {
	if strings.IndexFunc(s, badNameRune) >= 0 {
		return strconv.Quote(s)
	}
	return s
}

func (e *PathError) Unwrap() error { return e.Err }

// PathInfo is a path that may be added: Rel is workspace-relative with "/"
// separators and no trailing slash. Size is a file's size. Files is left
// for the caller to fill from its Index (ValidatePath does not walk). Warn
// and Deny are reasons in words: a warning shows, a denied path is added
// only once the user confirms.
type PathInfo struct {
	Rel   string
	Dir   bool
	Size  int64
	Files int
	Warn  string
	Deny  string
}

// denyPatterns are the base names that look like they hold secrets (D9),
// matched lower-cased with path.Match.
var denyPatterns = []string{
	".env", ".env.*", "*.pem", "*.key", "id_rsa*", "id_ed25519*", "*.p12",
	".netrc", ".npmrc", ".pypirc", "credentials*", "*.kdbx",
}

// denyReason is the words a denylisted row shows.
const denyReason = "looks like secrets"

// Denied reports whether rel's base name (a folder's too) looks like it
// holds secrets, and the reason in words.
func Denied(rel string) (reason string, ok bool) {
	base := strings.ToLower(path.Base(strings.TrimSuffix(filepath.ToSlash(rel), "/")))
	for _, pat := range denyPatterns {
		if hit, _ := path.Match(pat, base); hit {
			return denyReason, true
		}
	}
	return "", false
}

// badNameRune reports a rune a path in the prompt may not hold: a control
// (a newline would start a fake list line), U+2028/U+2029, a bidi or
// invisible rune, or invalid UTF-8.
func badNameRune(r rune) bool {
	return unicode.IsControl(r) || r == 0x2028 || r == 0x2029 || isBidi(r) || isInvisible(r) || r == utf8.RuneError
}

// inside reports whether target is base or under it, and the relative
// path. filepath.Rel, not a string prefix: "/ws-other" is not inside "/ws".
func inside(base, target string) (string, bool) {
	rel, err := filepath.Rel(base, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", false
	}
	return rel, true
}

// plainKind reports a mode that is a regular file or a folder.
func plainKind(m os.FileMode) bool { return m.IsRegular() || m.IsDir() }

// ValidatePath checks p (relative to root, or absolute) against the path
// rules: it returns the path's info, or a *PathError wrapping ErrBadName,
// ErrOutside (with the symlink target named) or ErrSpecial, or the file
// system's error (a missing file). Nothing is opened.
func ValidatePath(root, p string) (PathInfo, error) {
	info, _, _, err := validate(root, p)
	return info, err
}

// validate is ValidatePath that also returns the fully resolved path and
// its Lstat, for Preview to open and re-check.
func validate(root, p string) (PathInfo, string, os.FileInfo, error) {
	fail := func(err error) (PathInfo, string, os.FileInfo, error) {
		return PathInfo{}, "", nil, &PathError{Path: p, Err: err}
	}
	if strings.TrimSpace(p) == "" || strings.IndexFunc(p, badNameRune) >= 0 {
		return fail(ErrBadName)
	}
	if !filepath.IsAbs(root) {
		return fail(ErrOutside) // no workspace to be inside of
	}
	absRoot := filepath.Clean(root)
	full := filepath.Clean(filepath.FromSlash(p))
	if !filepath.IsAbs(full) {
		full = filepath.Join(absRoot, full)
	}
	rel, ok := inside(absRoot, full)
	switch {
	case !ok:
		return fail(ErrOutside)
	case rel == ".":
		return fail(ErrBadName) // the workspace itself is not an item
	}
	// Lstat first: a FIFO, socket or device is refused here, unopened.
	lfi, err := os.Lstat(full)
	if err == nil && lfi.Mode()&os.ModeSymlink == 0 && !plainKind(lfi.Mode()) {
		err = ErrSpecial
	}
	if err != nil {
		return fail(err)
	}
	realRoot, rootErr := filepath.EvalSymlinks(absRoot)
	real, err := filepath.EvalSymlinks(full)
	if err = errors.Join(rootErr, err); err != nil {
		return fail(err)
	}
	if _, ok := inside(realRoot, real); !ok {
		return PathInfo{}, "", nil, &PathError{Path: p, Target: real, Err: ErrOutside}
	}
	fi, err := os.Lstat(real)
	if err == nil && !plainKind(fi.Mode()) {
		err = ErrSpecial // a link to a FIFO or a device
	}
	if err != nil {
		return fail(err)
	}
	info := PathInfo{Rel: filepath.ToSlash(rel), Dir: fi.IsDir()}
	if !info.Dir {
		info.Size = fi.Size()
		if info.Size > BigFile {
			info.Warn = "the agent may not read all of it"
		}
	}
	// A harmless name that links to a secret is flagged too.
	if reason, ok := Denied(info.Rel); ok {
		info.Deny = reason
	} else if reason, ok := Denied(real); ok {
		info.Deny = reason
	}
	return info, real, fi, nil
}

// openRead opens a file for reading without blocking (open_unix.go); tests
// swap it.
var openRead = openNonBlocking

// Preview reads the head of the regular file rel for the preview: at most
// maxBytes, sanitized, split in lines, at most maxLines of them. more is
// how many lines are left out: those read past maxLines, plus one when the
// read stopped at maxBytes (the line it cut is left out too). The path is
// checked first (ValidatePath); the opened file must still be that regular
// file, or it is ErrSpecial. A folder is ErrIsDir and binary data
// ErrBinary.
func Preview(root, rel string, maxBytes, maxLines int) (lines []string, more int, err error) {
	info, real, fi, err := validate(root, rel)
	if err != nil {
		return nil, 0, err
	}
	if info.Dir {
		return nil, 0, &PathError{Path: rel, Err: ErrIsDir}
	}
	f, err := openRead(real)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = f.Close() }()
	// The file opened must be the one checked: the path may have been
	// swapped for something else in between.
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || !os.SameFile(st, fi) {
		return nil, 0, &PathError{Path: rel, Err: ErrSpecial}
	}
	b, err := io.ReadAll(io.LimitReader(f, int64(maxBytes)))
	if err != nil {
		return nil, 0, err
	}
	cut := st.Size() > int64(len(b))
	if cut {
		// The cap may split a rune; that is not binary data.
		for i := 0; i < utf8.UTFMax-1 && len(b) > 0 && !utf8.Valid(b); i++ {
			b = b[:len(b)-1]
		}
	}
	if IsBinary(string(b)) {
		return nil, 0, &PathError{Path: rel, Err: ErrBinary}
	}
	text := Sanitize(string(b))
	if cut {
		// The last line read is whole only when the read ended on a
		// newline; either way the file goes on.
		if i := strings.LastIndexByte(text, '\n'); i >= 0 {
			text = text[:i]
		} else {
			text = ""
		}
		more++
	}
	if text = strings.TrimRight(text, "\n"); text != "" {
		lines = strings.Split(text, "\n")
	}
	if len(lines) > maxLines {
		more += len(lines) - maxLines
		lines = lines[:maxLines]
	}
	return lines, more, nil
}
