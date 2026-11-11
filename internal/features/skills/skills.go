// Package skills is rex's skill library (~/.config/rex/skills/<name>/SKILL.md)
// and the staging of chosen skills into a per-session plugin folder, so an
// agent loads them for one session only and nothing is written to the
// agent's own config (~/.claude).
package skills

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"

	"gopkg.in/yaml.v3"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// SkillFile is the file every skill folder must hold.
const SkillFile = "SKILL.md"

// maxFrontmatter caps how much of a SKILL.md List reads to find its
// frontmatter.
const maxFrontmatter = 64 << 10

// maxDescription caps a listed description (display only).
const maxDescription = 200

var validName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// ValidName reports whether s is a legal skill name: lowercase letters,
// digits and hyphens, starting with a letter or digit, 1 to 64 bytes. It is
// also the folder name, so a valid name can never traverse out of the library.
func ValidName(s string) bool { return validName.MatchString(s) }

// Skill is one library entry.
type Skill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type frontmatter struct {
	Description string `yaml:"description"`
}

// List reads the skills under dir. A missing dir is an empty library. Entries
// that are not usable (symlinks, invalid names, no SKILL.md) are skipped and
// reported in errs. The result is sorted by name.
func List(dir string) (skills []Skill, errs []error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, []error{fmt.Errorf("open skills dir: %w", err)}
	}
	defer root.Close()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, []error{fmt.Errorf("read skills dir: %w", err)}
	}
	for _, e := range entries {
		name := e.Name()
		if e.Type()&os.ModeSymlink != 0 {
			errs = append(errs, fmt.Errorf("skill %q: symlinks are not followed", name))
			continue
		}
		if !e.IsDir() {
			continue
		}
		if !ValidName(name) {
			errs = append(errs, fmt.Errorf("skill %q: invalid name (use lowercase letters, digits and hyphens)", name))
			continue
		}
		desc, err := readDescription(root, name)
		if err != nil {
			errs = append(errs, fmt.Errorf("skill %q: %w", name, err))
			continue
		}
		skills = append(skills, Skill{Name: name, Description: desc})
	}
	sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
	return skills, errs
}

func readDescription(root *os.Root, name string) (string, error) {
	path := name + "/" + SkillFile
	fi, err := root.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("no %s", SkillFile)
	}
	if !fi.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a regular file", SkillFile)
	}
	f, err := root.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxFrontmatter))
	if err != nil {
		return "", err
	}
	return parseDescription(raw), nil
}

// parseDescription pulls `description` out of the leading `---` YAML block.
// A missing or malformed block gives an empty description, not an error: the
// skill still loads.
func parseDescription(raw []byte) string {
	raw = bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))
	raw = bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n"))
	rest, ok := bytes.CutPrefix(raw, []byte("---\n"))
	if !ok {
		return ""
	}
	block, _, ok := bytes.Cut(rest, []byte("\n---"))
	if !ok {
		return ""
	}
	var fm frontmatter
	if err := yaml.Unmarshal(block, &fm); err != nil {
		return ""
	}
	desc := protocol.FlattenDisplayText(fm.Description)
	if r := []rune(desc); len(r) > maxDescription {
		desc = string(r[:maxDescription-1]) + "…"
	}
	return desc
}
