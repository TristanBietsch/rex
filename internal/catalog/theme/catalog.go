package theme

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

// Issue is something wrong with one theme file, or a note about it.
type Issue struct {
	// File is the file's base name (the directory's, for a directory-wide
	// problem).
	File string
	// Msg says what is wrong. It never contains file contents.
	Msg string
	// Warn is true for advice (the file still loaded), false for a file
	// that was skipped.
	Warn bool
}

// String is `"name.yaml": message`. The file name is quoted (it is a raw
// directory entry, and may hold anything), so the result never carries a
// control character.
func (i Issue) String() string { return fmt.Sprintf("%q: %s", i.File, i.Msg) }

// printable replaces every control, bidirectional or non-printing character
// in s with '?'. Issue messages can echo file names and values from a file
// someone else wrote, and they reach the screen.
func printable(s string) string {
	return strings.Map(func(r rune) rune {
		if isBidi(r) || unicode.IsControl(r) || !unicode.IsPrint(r) && r != ' ' {
			return '?'
		}
		return r
	}, s)
}

// Catalog is the user's theme files, loaded from one directory. A nil
// *Catalog holds no user themes.
type Catalog struct {
	user map[string]Palette
	// Issues lists every skipped file and every warning, in file order.
	Issues []Issue
}

// Skipped returns the issues that made a file unusable (not the warnings).
func (c *Catalog) Skipped() []Issue {
	if c == nil {
		return nil
	}
	var out []Issue
	for _, i := range c.Issues {
		if !i.Warn {
			out = append(out, i)
		}
	}
	return out
}

// WhySkipped is why the file for theme name was not loaded, or "" when it was
// loaded or does not exist.
func (c *Catalog) WhySkipped(name string) string {
	for _, i := range c.Skipped() {
		if i.File == name+".yaml" {
			return i.Msg
		}
	}
	return ""
}

// Get returns the named theme: a user theme, else a built-in. An unknown name
// returns the default palette and false. The result is a copy.
func (c *Catalog) Get(name string) (Palette, bool) {
	if c != nil {
		if p, ok := c.user[name]; ok {
			p.Fleet = append([]string(nil), p.Fleet...)
			return p, true
		}
	}
	return Get(name)
}

// IsUser reports whether name is a user theme.
func (c *Catalog) IsUser(name string) bool {
	if c == nil {
		return false
	}
	_, ok := c.user[name]
	return ok
}

// UserNames returns the user theme names, sorted.
func (c *Catalog) UserNames() []string {
	if c == nil {
		return nil
	}
	names := make([]string, 0, len(c.user))
	for n := range c.user {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// LoadDir reads every <name>.yaml in dir. It never fails: a missing
// directory is an empty catalog, and anything unusable is skipped with an
// Issue so one bad file cannot hide the others or break the program. The
// rules, each of which skips a file:
//
//   - at most MaxFiles files, each at most MaxFileSize bytes;
//   - the name must pass ValidName and must not be a built-in;
//   - a symlink is followed (a stow link into a dotfiles repo is fine), but
//     what it resolves to must be a regular file that passes the same checks
//     as a file in dir; a dangling link or a link to anything else is
//     skipped. On Windows a link must stay inside dir;
//   - on unix, nothing owned by another user or writable by group or others,
//     the directory and every link's target included.
func LoadDir(dir string) *Catalog {
	c := loadDir(dir)
	for i := range c.Issues {
		c.Issues[i].Msg = printable(c.Issues[i].Msg)
	}
	return c
}

func loadDir(dir string) *Catalog {
	c := &Catalog{user: map[string]Palette{}}
	if dir == "" {
		return c
	}
	here := filepath.Base(dir)
	fi, err := os.Stat(dir)
	switch {
	case os.IsNotExist(err):
		return c
	case err != nil:
		c.Issues = append(c.Issues, Issue{File: here, Msg: "cannot read the themes directory: " + err.Error()})
		return c
	case !fi.IsDir():
		c.Issues = append(c.Issues, Issue{File: here, Msg: "not a directory"})
		return c
	}
	if err := checkPrivateDir(fi, dir); err != nil {
		c.Issues = append(c.Issues, Issue{File: here, Msg: err.Error() + "; no user themes loaded"})
		return c
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		c.Issues = append(c.Issues, Issue{File: here, Msg: "cannot read the themes directory: " + err.Error()})
		return c
	}
	n := 0
	for _, e := range entries { // sorted by name
		file := e.Name()
		if !strings.HasSuffix(file, ".yaml") {
			continue
		}
		if n++; n > MaxFiles {
			c.Issues = append(c.Issues, Issue{File: file, Msg: fmt.Sprintf("more than %d theme files; this and the rest are ignored", MaxFiles)})
			break
		}
		data, err := readThemeFile(dir, file)
		if err != nil {
			c.Issues = append(c.Issues, Issue{File: file, Msg: err.Error()})
			continue
		}
		name := strings.TrimSuffix(file, ".yaml")
		p, warns, err := Parse(name, data)
		if err != nil {
			c.Issues = append(c.Issues, Issue{File: file, Msg: err.Error()})
			continue
		}
		c.user[name] = p
		for _, w := range warns {
			c.Issues = append(c.Issues, Issue{File: file, Msg: w, Warn: true})
		}
	}
	return c
}

// readAll is io.ReadAll; tests replace it to fail a read.
var readAll = io.ReadAll

// readThemeFile reads one theme file. The file is opened once and every check
// runs on that handle (fstat), so what is checked is what is read: a link
// swapped in between cannot slip a different file past the checks. A symlink
// is followed; its target must be a regular file, owned by you and not
// writable by group or others, within the size cap. The open never blocks on
// a FIFO.
func readThemeFile(dir, file string) ([]byte, error) {
	f, err := openThemeFile(dir, file)
	if err != nil {
		return nil, fmt.Errorf("cannot open file (a dangling or looping link, or one that is not allowed): %w", err)
	}
	defer func() { _ = f.Close() }()
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		return nil, errors.New("not a regular file (or a link to one); refused")
	}
	if fi.Size() > MaxFileSize {
		return nil, fmt.Errorf("file is larger than %d KiB; refused", MaxFileSize>>10)
	}
	if err := checkPrivateFile(fi); err != nil {
		return nil, err
	}
	data, err := readAll(io.LimitReader(f, MaxFileSize+1)) // Parse refuses a file that grew past the cap
	if err != nil {
		return nil, fmt.Errorf("cannot read file: %w", err)
	}
	return data, nil
}

// Stamp summarizes the theme files in dir (names, sizes, modification times)
// as a string that changes when any of them is added, removed or edited. It
// stats paths, not inodes, so an editor that saves by rename is seen. A
// missing or unreadable directory has the empty stamp.
func Stamp(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var b strings.Builder
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		fi, err := os.Stat(filepath.Join(dir, e.Name()))
		if err != nil {
			fmt.Fprintf(&b, "%s:?;", e.Name()) // dangling link: still a change when it heals
			continue
		}
		fmt.Fprintf(&b, "%s:%d:%d;", e.Name(), fi.Size(), fi.ModTime().UnixNano())
	}
	return b.String()
}
