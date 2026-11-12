// Package loop runs repeat loop workflows: one agent session that is sent a
// prompt again each time it finishes a turn, until a check command exits 0,
// a marker line shows up in its output, or a cap is reached.
//
// This file is the loop file format and its validator. A loop file is YAML
// in ~/.config/rex/loops/<name>.yaml. Loop files run commands, so only the
// user's own files are read: a symlink, a file another user owns, and a
// group- or world-writable file are refused.
package loop

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"

	"gopkg.in/yaml.v3"
)

// Limits and defaults of the file format.
const (
	KindRepeat = "repeat"

	DefaultCheckTimeout  = 15 * time.Minute
	MaxCheckTimeout      = 2 * time.Hour
	DefaultMaxIterations = 10
	MaxMaxIterations     = 100
	DefaultMaxMinutes    = 120
	MaxMaxMinutes        = 1440

	// MaxFileBytes bounds a loop file.
	MaxFileBytes = 64 << 10
	// MaxPromptBytes bounds a prompt.
	MaxPromptBytes = 32 << 10

	maxNameLen   = 64
	maxCheckArgs = 64
	maxArgBytes  = 4096
	maxMarkerLen = 512
	maxFieldLen  = 128
)

var nameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

// ValidName reports whether name can be a loop name (and so a file stem):
// lowercase letters, digits, '.', '_' and '-', at most 64 bytes, starting
// with a letter or digit.
func ValidName(name string) bool {
	return len(name) <= maxNameLen && nameRE.MatchString(name)
}

// Def is a validated loop definition. Build it with Parse or LoadFile.
type Def struct {
	Name           string `json:"name"`
	Kind           string `json:"kind"`
	Tool           string `json:"tool"`
	Model          string `json:"model"`
	Effort         string `json:"effort,omitempty"`
	CWD            string `json:"cwd,omitempty"`
	Prompt         string `json:"prompt"`
	FollowupPrompt string `json:"followup_prompt"`
	Until          Until  `json:"until"`
}

// Until is a loop's stop conditions and caps. Defaults are filled in.
type Until struct {
	// Check is the argv of the check command; empty when only a marker is
	// used. Never a shell string.
	Check        []string      `json:"check,omitempty"`
	CheckTimeout time.Duration `json:"check_timeout"`
	// Marker is a regex that must match a whole output line; "" for none.
	Marker        string `json:"marker,omitempty"`
	MaxIterations int    `json:"max_iterations"`
	MaxMinutes    int    `json:"max_minutes"`

	markerRE *regexp.Regexp
}

// Compile prepares a Def decoded from JSON (the persisted state) for use. A
// Def from Parse is already compiled.
func (d *Def) Compile() error {
	re, err := compileMarker(d.Until.Marker)
	if err != nil {
		return err
	}
	d.Until.markerRE = re
	return nil
}

// MatchMarker reports whether any whole line of output matches the marker.
// Lines are compared without surrounding whitespace, so the marker must be
// on a line by itself: a sentence that quotes it does not match.
func (u Until) MatchMarker(output string) bool {
	if u.markerRE == nil {
		return false
	}
	for line := range strings.SplitSeq(output, "\n") {
		if u.markerRE.MatchString(strings.TrimSpace(line)) {
			return true
		}
	}
	return false
}

// compileMarker anchors the user's regex to a whole line.
func compileMarker(marker string) (*regexp.Regexp, error) {
	if marker == "" {
		return nil, nil
	}
	if len(marker) > maxMarkerLen {
		return nil, fmt.Errorf("until.marker is longer than %d bytes", maxMarkerLen)
	}
	re, err := regexp.Compile(`^(?:` + marker + `)$`)
	if err != nil {
		return nil, fmt.Errorf("until.marker is not a valid regex: %w", err)
	}
	if re.MatchString("") {
		return nil, errors.New("until.marker matches an empty line; it must match text")
	}
	return re, nil
}

// argv is the check command: a YAML list, never a scalar.
type argv []string

func (a *argv) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.SequenceNode {
		return errors.New("until.check must be a list of arguments (for example [\"make\", \"check\"]), not a shell string")
	}
	out := make(argv, 0, len(n.Content))
	for _, c := range n.Content {
		if c.Kind != yaml.ScalarNode || c.Tag == "!!null" {
			return errors.New("until.check entries must be text")
		}
		out = append(out, c.Value)
	}
	*a = out
	return nil
}

type rawDef struct {
	Name           string   `yaml:"name"`
	Kind           string   `yaml:"kind"`
	Tool           string   `yaml:"tool"`
	Model          string   `yaml:"model"`
	Effort         string   `yaml:"effort"`
	CWD            string   `yaml:"cwd"`
	Prompt         string   `yaml:"prompt"`
	FollowupPrompt string   `yaml:"followup_prompt"`
	Until          rawUntil `yaml:"until"`
}

type rawUntil struct {
	Check         *argv  `yaml:"check"`
	CheckTimeout  string `yaml:"check_timeout"`
	Marker        string `yaml:"marker"`
	MaxIterations *int   `yaml:"max_iterations"`
	MaxMinutes    *int   `yaml:"max_minutes"`
}

// Parse validates a loop file's bytes. stem is the file name without
// ".yaml"; the file's name field must equal it.
func Parse(data []byte, stem string) (Def, error) {
	if len(data) > MaxFileBytes {
		return Def{}, fmt.Errorf("loop file too large (limit %d bytes)", MaxFileBytes)
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var raw rawDef
	if err := dec.Decode(&raw); err != nil {
		if errors.Is(err, io.EOF) {
			return Def{}, errors.New("loop file is empty")
		}
		return Def{}, fmt.Errorf("yaml: %w", err)
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); err == nil {
		return Def{}, errors.New("yaml: a loop file holds one document")
	}

	if !ValidName(raw.Name) {
		return Def{}, fmt.Errorf("name %q is invalid: use lowercase letters, digits, '.', '_' or '-' (at most %d)", raw.Name, maxNameLen)
	}
	if raw.Name != stem {
		return Def{}, fmt.Errorf("name %q must match the file name %q", raw.Name, stem+".yaml")
	}
	switch raw.Kind {
	case KindRepeat:
	case "chain":
		return Def{}, errors.New("kind chain is not supported yet; use repeat")
	default:
		return Def{}, fmt.Errorf("kind must be %q (got %q)", KindRepeat, raw.Kind)
	}
	for _, f := range []struct{ name, val string }{{"tool", raw.Tool}, {"model", raw.Model}, {"effort", raw.Effort}} {
		if err := checkShortField(f.name, f.val, f.name != "effort"); err != nil {
			return Def{}, err
		}
	}
	if raw.CWD != "" {
		if !filepath.IsAbs(raw.CWD) || slices.Contains(strings.FieldsFunc(raw.CWD, func(r rune) bool { return r == '/' || r == '\\' }), "..") || strings.ContainsRune(raw.CWD, 0) {
			return Def{}, fmt.Errorf("cwd %q must be an absolute path without \"..\"", raw.CWD)
		}
	}
	if err := checkPrompt("prompt", raw.Prompt, true); err != nil {
		return Def{}, err
	}
	if err := checkPrompt("followup_prompt", raw.FollowupPrompt, false); err != nil {
		return Def{}, err
	}

	d := Def{
		Name: raw.Name, Kind: raw.Kind, Tool: raw.Tool, Model: raw.Model, Effort: raw.Effort,
		CWD: raw.CWD, Prompt: raw.Prompt, FollowupPrompt: raw.FollowupPrompt,
	}
	if d.FollowupPrompt == "" {
		d.FollowupPrompt = d.Prompt
	}
	u, err := parseUntil(raw.Until)
	if err != nil {
		return Def{}, err
	}
	d.Until = u
	if d.Until.markerRE != nil {
		for _, p := range []struct{ name, text string }{{"prompt", d.Prompt}, {"followup_prompt", d.FollowupPrompt}} {
			if d.Until.MatchMarker(p.text) {
				return Def{}, fmt.Errorf("until.marker matches a whole line of %s, so the loop would stop at once: the agent echoes the prompt", p.name)
			}
		}
	}
	return d, nil
}

func parseUntil(r rawUntil) (Until, error) {
	u := Until{
		CheckTimeout:  DefaultCheckTimeout,
		MaxIterations: DefaultMaxIterations,
		MaxMinutes:    DefaultMaxMinutes,
		Marker:        r.Marker,
	}
	if r.Check != nil {
		c := []string(*r.Check)
		switch {
		case len(c) == 0:
			return u, errors.New("until.check must not be empty")
		case len(c) > maxCheckArgs:
			return u, fmt.Errorf("until.check has more than %d arguments", maxCheckArgs)
		case strings.TrimSpace(c[0]) == "":
			return u, errors.New("until.check: the command (first entry) is empty")
		case len(c) == 1 && strings.ContainsFunc(c[0], unicode.IsSpace):
			return u, errors.New("until.check looks like a shell string; split it into one entry per argument")
		}
		for _, a := range c {
			if len(a) > maxArgBytes || strings.ContainsRune(a, 0) {
				return u, errors.New("until.check has an argument that is too long or holds a NUL byte")
			}
		}
		u.Check = slices.Clone(c)
	}
	if u.Check == nil && r.Marker == "" {
		return u, errors.New("until needs a check or marker (at least one stop condition)")
	}
	if r.CheckTimeout != "" {
		d, err := time.ParseDuration(r.CheckTimeout)
		if err != nil || d <= 0 || d > MaxCheckTimeout {
			return u, fmt.Errorf("until.check_timeout %q must be a duration from 1s to %s (for example 15m)", r.CheckTimeout, MaxCheckTimeout)
		}
		u.CheckTimeout = d
	}
	if err := intIn("until.max_iterations", r.MaxIterations, 1, MaxMaxIterations, &u.MaxIterations); err != nil {
		return u, err
	}
	if err := intIn("until.max_minutes", r.MaxMinutes, 1, MaxMaxMinutes, &u.MaxMinutes); err != nil {
		return u, err
	}
	re, err := compileMarker(u.Marker)
	if err != nil {
		return u, err
	}
	u.markerRE = re
	return u, nil
}

func intIn(name string, p *int, lo, hi int, dst *int) error {
	if p == nil {
		return nil
	}
	if *p < lo || *p > hi {
		return fmt.Errorf("%s must be %d to %d (got %d)", name, lo, hi, *p)
	}
	*dst = *p
	return nil
}

func checkShortField(name, v string, required bool) error {
	if v == "" {
		if required {
			return fmt.Errorf("%s is required", name)
		}
		return nil
	}
	if len(v) > maxFieldLen || strings.ContainsFunc(v, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }) {
		return fmt.Errorf("%s %q is invalid: no spaces or control characters, at most %d bytes", name, v, maxFieldLen)
	}
	return nil
}

func checkPrompt(name, v string, required bool) error {
	if strings.TrimSpace(v) == "" {
		if required {
			return fmt.Errorf("%s is required", name)
		}
		return nil
	}
	if len(v) > MaxPromptBytes {
		return fmt.Errorf("%s is longer than %d bytes", name, MaxPromptBytes)
	}
	if strings.ContainsRune(v, 0) {
		return fmt.Errorf("%s holds a NUL byte", name)
	}
	return nil
}

// FilePath is where the loop called name lives under dir.
func FilePath(dir, name string) string { return filepath.Join(dir, name+".yaml") }

// LoadFile reads and validates dir/<name>.yaml. It refuses a bad name, a
// symlink, anything that is not a regular file, a file another user owns
// and a group- or world-writable file. A missing file wraps os.ErrNotExist.
func LoadFile(dir, name string) (Def, error) {
	if !ValidName(name) {
		return Def{}, fmt.Errorf("loop name %q is invalid: use lowercase letters, digits, '.', '_' or '-'", name)
	}
	path := FilePath(dir, name)
	fi, err := os.Lstat(path)
	if err != nil {
		return Def{}, fmt.Errorf("loop file: %w", err)
	}
	if fi.Mode()&fs.ModeSymlink != 0 {
		return Def{}, fmt.Errorf("%s is a symlink; loop files must be regular files", path)
	}
	if !fi.Mode().IsRegular() {
		return Def{}, fmt.Errorf("%s is not a regular file", path)
	}
	if err := checkOwnerAndMode(fi); err != nil {
		return Def{}, fmt.Errorf("%s: %w", path, err)
	}
	f, err := os.Open(path)
	if err != nil {
		return Def{}, fmt.Errorf("loop file: %w", err)
	}
	defer f.Close()
	// The path may have been swapped between Lstat and Open.
	of, err := f.Stat()
	if err != nil || !os.SameFile(fi, of) {
		return Def{}, fmt.Errorf("%s changed while it was read", path)
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxFileBytes+1))
	if err != nil {
		return Def{}, fmt.Errorf("read %s: %w", path, err)
	}
	d, err := Parse(data, name)
	if err != nil {
		return Def{}, fmt.Errorf("%s: %w", path, err)
	}
	return d, nil
}

// Entry is one file found by List: its Def, or why it is not usable.
type Entry struct {
	Name string
	Def  Def
	Err  error
}

// List reads every *.yaml in dir, sorted by name. A missing dir lists
// nothing. A file that fails validation is an Entry with Err set.
func List(dir string) ([]Entry, error) {
	des, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read loops dir: %w", err)
	}
	var out []Entry
	for _, de := range des {
		stem, ok := strings.CutSuffix(de.Name(), ".yaml")
		if !ok {
			continue
		}
		e := Entry{Name: stem}
		e.Def, e.Err = LoadFile(dir, stem)
		out = append(out, e)
	}
	slices.SortFunc(out, func(a, b Entry) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

// Starter is the commented starter file `rex loop new` writes.
func Starter(name string) string {
	return `# Loop workflow: ` + name + `
#
# rex re-sends the prompt each time the agent finishes a turn, until the
# check command exits 0 (or the marker shows up), or a cap is reached.
# This file runs commands on your machine: keep it yours (chmod 600).
name: ` + name + `
kind: repeat
tool: claude
model: sonnet
# effort: high
# cwd: /absolute/path/to/project   (else rex loop start -cwd, else the current folder)
prompt: "Run make check and fix the first failure. Stop when it passes."
# followup_prompt: "Continue: run make check again and fix the next failure."
until:
  # Command to run in the session folder after every turn. A list, never a
  # shell string. Exit 0 ends the loop as done.
  check: ["make", "check"]
  # check_timeout: 15m        # default 15m, at most 2h
  # A regex that must match a whole output line (not a quote inside a
  # sentence). It must not match any line of your own prompt.
  # marker: '<promise>DONE</promise>'
  max_iterations: 8           # default 10, at most 100
  max_minutes: 120            # default 120, at most 1440
`
}
