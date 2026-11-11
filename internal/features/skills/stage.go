package skills

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
)

// Staging caps: a skill is a few markdown files, never a data dump.
const (
	MaxFiles = 256
	MaxBytes = 4 << 20
	// MaxPerSession is how many skills one session may name.
	MaxPerSession = 16
)

// PluginDirName is the plugin folder inside a session directory.
const PluginDirName = "plugin"

const pluginManifest = `{"name":"rex","description":"skills rex added to this session","version":"1"}` + "\n"

// ValidateNames checks a requested skill list: at most MaxPerSession, all
// valid, no duplicates.
func ValidateNames(names []string) error {
	if len(names) > MaxPerSession {
		return fmt.Errorf("too many skills (%d, max %d)", len(names), MaxPerSession)
	}
	seen := make(map[string]struct{}, len(names))
	for _, n := range names {
		if !ValidName(n) {
			return fmt.Errorf("invalid skill name %q (use lowercase letters, digits and hyphens)", n)
		}
		if _, dup := seen[n]; dup {
			return fmt.Errorf("duplicate skill %q", n)
		}
		seen[n] = struct{}{}
	}
	return nil
}

// Stage copies the named skills from the library lib into a plugin folder
// <sessionDir>/plugin and returns its path. An existing plugin folder is
// replaced, so a resume re-stages from the current library.
//
// The library is read and the plugin written only through os.Root, so a name
// or a symlink cannot reach outside either tree. Only regular files and
// folders are copied (a symlink anywhere in a skill is an error), at most
// MaxFiles files and MaxBytes bytes in total. rex writes the plugin manifest
// itself, so library content cannot bring hooks or MCP servers along.
func Stage(lib, sessionDir string, names []string) (string, error) {
	if len(names) == 0 {
		return "", errors.New("no skills to stage")
	}
	if err := ValidateNames(names); err != nil {
		return "", err
	}
	src, err := os.OpenRoot(lib)
	if err != nil {
		return "", fmt.Errorf("open skills library: %w", err)
	}
	defer src.Close()
	// Fail on a missing or unusable skill before touching the session dir.
	for _, n := range names {
		fi, err := src.Lstat(n + "/" + SkillFile)
		if err != nil || !fi.Mode().IsRegular() {
			return "", fmt.Errorf("skill %q not found in the library", n)
		}
	}

	plugin := filepath.Join(sessionDir, PluginDirName)
	if err := os.RemoveAll(plugin); err != nil {
		return "", fmt.Errorf("clear plugin dir: %w", err)
	}
	if err := os.MkdirAll(plugin, 0o700); err != nil {
		return "", fmt.Errorf("create plugin dir: %w", err)
	}
	if err := stageInto(src, plugin, names); err != nil {
		_ = os.RemoveAll(plugin)
		return "", err
	}
	return plugin, nil
}

func stageInto(src *os.Root, plugin string, names []string) error {
	dst, err := os.OpenRoot(plugin)
	if err != nil {
		return fmt.Errorf("open plugin dir: %w", err)
	}
	defer dst.Close()
	if err := dst.Mkdir(".claude-plugin", 0o700); err != nil {
		return err
	}
	if err := writeFile(dst, ".claude-plugin/plugin.json", []byte(pluginManifest)); err != nil {
		return err
	}
	var files, total int64
	srcFS := src.FS()
	for _, n := range names {
		err := fs.WalkDir(srcFS, n, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel := path.Join("skills", p)
			switch {
			case d.Type()&fs.ModeSymlink != 0:
				return fmt.Errorf("skill %q: %s is a symlink (not allowed)", n, p)
			case d.IsDir():
				return dst.MkdirAll(rel, 0o700)
			case d.Type().IsRegular():
				files++
				if files > MaxFiles {
					return fmt.Errorf("skills too large: more than %d files", MaxFiles)
				}
				copied, err := copyFile(src, dst, p, rel, MaxBytes-total)
				total += copied
				return err
			default:
				return fmt.Errorf("skill %q: %s is not a regular file", n, p)
			}
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// copyFile copies src:from to dst:to, refusing more than budget bytes. It
// returns the bytes written.
func copyFile(src, dst *os.Root, from, to string, budget int64) (int64, error) {
	in, err := src.Open(from)
	if err != nil {
		return 0, err
	}
	defer in.Close()
	fi, err := in.Stat()
	if err != nil {
		return 0, err
	}
	if !fi.Mode().IsRegular() {
		return 0, fmt.Errorf("%s is not a regular file", from)
	}
	out, err := dst.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(out, io.LimitReader(in, budget+1))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return n, err
	}
	if n > budget {
		return n, fmt.Errorf("skills too large: more than %d bytes", MaxBytes)
	}
	return n, nil
}

func writeFile(root *os.Root, name string, data []byte) error {
	f, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}
