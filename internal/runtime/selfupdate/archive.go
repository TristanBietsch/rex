package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Staged holds the new binaries as temp files in the install folder, ready
// to be renamed over the old ones.
type Staged struct {
	dir  string
	goos string
	// tmp maps a binary's file name (rex, rex-daemon.exe, …) to its temp file.
	tmp map[string]string
	// order is the rename order: rex-daemon first, rex last.
	order []string
}

// binaries are the files Stage extracts, in install order.
func binaries(goos string) []string {
	ext := ""
	if goos == "windows" {
		ext = ".exe"
	}
	return []string{"rex-daemon" + ext, "rex" + ext}
}

// Stage extracts rex and rex-daemon from the archive into temp files in
// dir (the install folder, so the final rename stays on one filesystem).
// Every entry of the archive is checked; any link, path outside the top
// folder, nested path or oversized entry refuses the whole archive and
// leaves dir as it was.
func (a *Archive) Stage(dir string) (*Staged, error) {
	st := &Staged{dir: dir, goos: a.goos, tmp: map[string]string{}, order: binaries(a.goos)}
	var err error
	if strings.HasSuffix(a.Name, ".zip") {
		err = a.stageZip(st)
	} else {
		err = a.stageTarGz(st)
	}
	if err == nil {
		for _, b := range st.order {
			if _, ok := st.tmp[b]; !ok {
				err = fmt.Errorf("%s does not contain %s", a.Name, b)
				break
			}
		}
	}
	if err != nil {
		st.Cleanup()
		return nil, err
	}
	return st, nil
}

// wanted checks an archive entry's name and type and returns the binary it
// is ("" for another file such as LICENSE, or the top folder itself).
func (a *Archive) wanted(st *Staged, name string, isDir, isReg bool, size int64) (string, error) {
	clean := strings.TrimSuffix(name, "/")
	if strings.Contains(name, "\\") || path.IsAbs(name) || path.Clean(clean) != clean || clean == "" {
		return "", fmt.Errorf("%s: unexpected path %q", a.Name, name)
	}
	for _, el := range strings.Split(clean, "/") {
		if el == ".." || el == "." {
			return "", fmt.Errorf("%s: unexpected path %q", a.Name, name)
		}
	}
	if clean == a.dir && isDir {
		return "", nil
	}
	rest, ok := strings.CutPrefix(clean, a.dir+"/")
	if !ok || strings.Contains(rest, "/") {
		return "", fmt.Errorf("%s: unexpected path %q", a.Name, name)
	}
	if !isReg {
		return "", fmt.Errorf("%s: %q is not a regular file", a.Name, name)
	}
	if size < 0 || size > a.limit {
		return "", fmt.Errorf("%s: %q is too large (%d bytes, limit %d)", a.Name, name, size, a.limit)
	}
	for _, b := range st.order {
		if rest == b {
			if _, dup := st.tmp[b]; dup {
				return "", fmt.Errorf("%s: %q appears twice", a.Name, name)
			}
			return b, nil
		}
	}
	return "", nil
}

func (a *Archive) stageTarGz(st *Staged) error {
	f, err := os.Open(a.Path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("%s: %w", a.Name, err)
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	for n := 0; ; n++ {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("%s: %w", a.Name, err)
		}
		if n >= maxEntries {
			return fmt.Errorf("%s: too many entries", a.Name)
		}
		bin, err := a.wanted(st, h.Name, h.Typeflag == tar.TypeDir, h.Typeflag == tar.TypeReg, h.Size)
		if err != nil {
			return err
		}
		if bin == "" {
			continue // tar.Reader skips the body on Next
		}
		if err := a.write(st, bin, tr); err != nil {
			return err
		}
	}
}

func (a *Archive) stageZip(st *Staged) error {
	zr, err := zip.OpenReader(a.Path)
	if err != nil {
		return fmt.Errorf("%s: %w", a.Name, err)
	}
	defer func() { _ = zr.Close() }()
	if len(zr.File) > maxEntries {
		return fmt.Errorf("%s: too many entries", a.Name)
	}
	for _, zf := range zr.File {
		// FileInfo().Size() is the declared uncompressed size; a value past
		// int64 comes back negative, which wanted refuses. write enforces
		// the limit on the bytes actually read.
		mode := zf.Mode()
		bin, err := a.wanted(st, zf.Name, mode.IsDir(), mode.IsRegular(), zf.FileInfo().Size())
		if err != nil {
			return err
		}
		if bin == "" {
			continue
		}
		rc, err := zf.Open()
		if err != nil {
			return fmt.Errorf("%s: %w", a.Name, err)
		}
		err = a.write(st, bin, rc)
		_ = rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// write copies at most the entry limit from r into a new temp file in the
// install folder, syncs it and makes it executable.
func (a *Archive) write(st *Staged, bin string, r io.Reader) (err error) {
	f, err := os.CreateTemp(st.dir, ".rex-update-*")
	if err != nil {
		return fmt.Errorf("cannot write to %s: %w", st.dir, err)
	}
	st.tmp[bin] = f.Name()
	defer func() {
		if cerr := f.Close(); err == nil && cerr != nil {
			err = cerr
		}
	}()
	n, err := io.Copy(f, io.LimitReader(r, a.limit+1))
	if err != nil {
		return fmt.Errorf("%s: extracting %s: %w", a.Name, bin, err)
	}
	if n > a.limit {
		return fmt.Errorf("%s: %s is too large (over %d bytes)", a.Name, bin, a.limit)
	}
	if err := f.Chmod(0o755); err != nil {
		return err
	}
	return f.Sync()
}

// Cleanup removes the temp files that were not installed.
func (st *Staged) Cleanup() {
	for b, p := range st.tmp {
		_ = os.Remove(p)
		delete(st.tmp, b)
	}
}

// Install renames the new binaries over the old ones, rex-daemon first.
// Each rename is atomic: a reader sees the old file or the new one, never
// a partial one. On Windows a running .exe cannot be replaced but can be
// renamed, so the old binary is first moved aside to <name>.old (a stale
// .old from an earlier update is removed first).
func (st *Staged) Install() error {
	defer st.Cleanup()
	for _, b := range st.order {
		target := filepath.Join(st.dir, b)
		if err := replace(st.tmp[b], target, st.goos == "windows"); err != nil {
			return fmt.Errorf("installing %s: %w", target, err)
		}
		delete(st.tmp, b)
	}
	syncDir(st.dir)
	return nil
}

func replace(tmp, target string, aside bool) error {
	if !aside {
		return os.Rename(tmp, target)
	}
	old := target + ".old"
	if err := os.Remove(old); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("removing %s: %w", old, err)
	}
	moved := false
	if _, err := os.Lstat(target); err == nil {
		if err := os.Rename(target, old); err != nil {
			return err
		}
		moved = true
	}
	if err := os.Rename(tmp, target); err != nil {
		if moved {
			_ = os.Rename(old, target)
		}
		return err
	}
	return nil
}

// syncDir makes the renames durable (best effort; not possible on Windows).
func syncDir(dir string) {
	d, err := os.Open(dir)
	if err != nil {
		return
	}
	_ = d.Sync()
	_ = d.Close()
}
