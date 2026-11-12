package settings

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"

	"gopkg.in/yaml.v3"

	"github.com/tristanbietsch/rex/internal/runtime/paths"
)

// Store holds the effective settings: defaults overlaid with values loaded from
// config.yaml (paths.ConfigFile).
type Store struct {
	values map[string]any
	src    map[string]Source
	// dirty holds the ids changed since the last Save: true for Set (Save
	// writes the key), false for Reset (Save deletes it).
	dirty map[string]bool
}

// Source says where a setting's current value comes from.
type Source string

const (
	// SourceDefault is the registry default: the key is not in config.yaml.
	SourceDefault Source = "default"
	// SourceFile is a value read from, or destined for, config.yaml.
	SourceFile Source = "file"
)

// errNoPath is Save's answer to an empty path, so a caller whose config
// path could not be resolved never writes into the working directory.
var errNoPath = errors.New("settings: no config file path")

// LoadDefault returns a Store holding the defaults overlaid with the user's
// config file, and that file's path (paths.ConfigFile). A missing file is
// not an error. On error the Store still holds the defaults, so best-effort
// callers can use it; path is "" when the home directory is unknown.
func LoadDefault() (store *Store, path string, err error) {
	store = NewStore()
	path, err = paths.ConfigFile()
	if err != nil {
		return store, "", fmt.Errorf("settings: %w", err)
	}
	return store, path, store.Load(path)
}

// NewStore returns a Store populated with the registry's defaults.
func NewStore() *Store {
	s := &Store{
		values: make(map[string]any, len(Registry)),
		src:    make(map[string]Source, len(Registry)),
		dirty:  make(map[string]bool),
	}
	for _, r := range Registry {
		s.values[r.ID] = r.Default
		s.src[r.ID] = SourceDefault
	}
	return s
}

// Load reads values from path and applies them on top of defaults. Missing file = no-op.
func (s *Store) Load(path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read %s: %w", path, err)
	}
	var data map[string]any
	if err := yaml.Unmarshal(raw, &data); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	for k, v := range data {
		setting, ok := Find(k)
		if !ok {
			// An alias (an older key name) counts only when the canonical
			// key is absent from the file.
			if setting, ok = findByAlias(k); !ok {
				continue // tolerate unknown keys for forward compat
			}
			if _, both := data[setting.ID]; both {
				continue
			}
		}
		coerced, err := coerce(setting, v)
		if err != nil {
			continue // skip invalid entries silently
		}
		s.values[setting.ID] = coerced
		s.src[setting.ID] = SourceFile
	}
	return nil
}

// findByAlias returns the setting that lists key as an alias.
func findByAlias(key string) (Setting, bool) {
	for _, r := range Registry {
		if slices.Contains(r.Aliases, key) {
			return r, true
		}
	}
	return Setting{}, false
}

// Save writes this Store's changes to path atomically (temp file, then
// rename). It re-reads the file and applies only the ids changed on this
// Store since it was created or last saved (Set writes the key, Reset
// deletes it), so another process's edit to a different key survives and
// defaults are never pinned into the file. Keys this build does not know
// are kept. An unparseable file is an error and is left untouched. A
// missing directory is created 0700. A new file is 0600; an existing one
// keeps its permission bits, so Save never widens a private config.yaml.
//
// There is no file lock: an edit landing between the read and the rename is
// lost, a much smaller window than the whole-file overwrite it replaces.
func (s *Store) Save(path string) error {
	if path == "" {
		return errNoPath
	}
	// A symlinked config.yaml (a dotfiles manager such as stow) is saved
	// through the link: renaming over the link itself would replace it
	// with a plain file and quietly detach the config from its repo. A
	// dangling link is an error, never clobbered.
	if fi, err := os.Lstat(path); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		real, err := filepath.EvalSymlinks(path)
		if err != nil {
			return fmt.Errorf("resolve %s: %w", path, err)
		}
		path = real
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	data := map[string]any{}
	raw, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := yaml.Unmarshal(raw, &data); err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		if data == nil {
			data = map[string]any{}
		}
	case !os.IsNotExist(err):
		return fmt.Errorf("read %s: %w", path, err)
	}
	for id, set := range s.dirty {
		if set {
			data[id] = s.values[id]
		} else {
			delete(data, id)
		}
		if setting, ok := Find(id); ok {
			for _, alias := range setting.Aliases {
				delete(data, alias) // the canonical key replaces the old name
			}
		}
	}
	b, err := yaml.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	mode := os.FileMode(0o600)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	// A temp file of its own (0600, never a leftover or a planted link),
	// so two processes saving at once can't write or rename each other's.
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create tmp: %w", err)
	}
	tmp := f.Name()
	_, werr := f.Write(b)
	if err := errors.Join(werr, f.Close()); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := os.Chmod(tmp, mode); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("chmod tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	clear(s.dirty)
	return nil
}

// Get returns the current value or nil.
func (s *Store) Get(id string) any { return s.values[id] }

// String returns the value as a string for display.
func (s *Store) String(id string) string {
	v := s.values[id]
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%v", v)
}

// Source reports where id's value comes from (SourceDefault for unknown ids).
func (s *Store) Source(id string) Source {
	if src, ok := s.src[id]; ok {
		return src
	}
	return SourceDefault
}

// Validate coerces raw to the Go type of setting id, checks it against the
// setting's options and range, and applies its Normalize func. It is what
// Set runs, exported for callers that must check a value without storing it.
func Validate(id string, raw any) (any, error) {
	setting, ok := Find(id)
	if !ok {
		return nil, fmt.Errorf("unknown setting %q", id)
	}
	return validate(setting, raw)
}

func validate(setting Setting, raw any) (any, error) {
	v, err := coerce(setting, raw)
	if err != nil {
		return nil, err
	}
	if str, ok := v.(string); ok && setting.Normalize != nil {
		if str, err = setting.Normalize(str); err != nil {
			return nil, fmt.Errorf("%s: %w", setting.ID, err)
		}
		v = str
	}
	return v, nil
}

// Set validates the new value against the Setting spec and stores it. The
// next Save writes the key.
func (s *Store) Set(id string, raw any) error {
	setting, ok := Find(id)
	if !ok {
		return fmt.Errorf("unknown setting %q", id)
	}
	if setting.ReadOnly {
		return fmt.Errorf("setting %q is read-only", id)
	}
	v, err := validate(setting, raw)
	if err != nil {
		return err
	}
	s.values[id] = v
	s.src[id] = SourceFile
	s.dirty[id] = true
	return nil
}

// Reset restores the setting to its default. The next Save deletes the key.
func (s *Store) Reset(id string) error {
	setting, ok := Find(id)
	if !ok {
		return fmt.Errorf("unknown setting %q", id)
	}
	s.values[id] = setting.Default
	s.src[id] = SourceDefault
	s.dirty[id] = false
	return nil
}

// Snapshot returns a copy of the current values.
func (s *Store) Snapshot() map[string]any {
	out := make(map[string]any, len(s.values))
	for k, v := range s.values {
		out[k] = v
	}
	return out
}

// WithDefaults returns a copy of s in which each of ids holds its registry
// default. s is not changed and the copy carries no dirty marks. An id that
// is not a setting is a programming error and panics.
func (s *Store) WithDefaults(ids ...string) *Store {
	c := NewStore()
	for k, v := range s.values {
		c.values[k] = v
		c.src[k] = s.src[k]
	}
	for _, id := range ids {
		def, ok := Find(id)
		if !ok {
			panic("settings: WithDefaults: unknown setting " + id)
		}
		c.values[id] = def.Default
		c.src[id] = SourceDefault
	}
	return c
}

// coerce converts raw input (typically string-from-CLI or interface{}-from-YAML) into the
// concrete Go type expected by the setting and validates it against options/range.
func coerce(setting Setting, raw any) (any, error) {
	switch setting.Type {
	case TypeBool:
		switch v := raw.(type) {
		case bool:
			return v, nil
		case string:
			b, err := strconv.ParseBool(v)
			if err != nil {
				return nil, fmt.Errorf("%s: %q is not a bool", setting.ID, v)
			}
			return b, nil
		}
		return nil, fmt.Errorf("%s: expected bool, got %T", setting.ID, raw)

	case TypeInt:
		var i int
		switch v := raw.(type) {
		case int:
			i = v
		case int64:
			i = int(v)
		case float64:
			i = int(v)
		case string:
			parsed, err := strconv.Atoi(v)
			if err != nil {
				return nil, fmt.Errorf("%s: %q is not an int", setting.ID, v)
			}
			i = parsed
		default:
			return nil, fmt.Errorf("%s: expected int, got %T", setting.ID, raw)
		}
		if (setting.Min != 0 || setting.Max != 0) && (float64(i) < setting.Min || float64(i) > setting.Max) {
			return nil, fmt.Errorf("%s: %d out of range [%v, %v]", setting.ID, i, setting.Min, setting.Max)
		}
		return i, nil

	case TypeFloat:
		var f float64
		switch v := raw.(type) {
		case float64:
			f = v
		case int:
			f = float64(v)
		case string:
			parsed, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return nil, fmt.Errorf("%s: %q is not a float", setting.ID, v)
			}
			f = parsed
		default:
			return nil, fmt.Errorf("%s: expected float, got %T", setting.ID, raw)
		}
		if setting.Max > setting.Min && (f < setting.Min || f > setting.Max) {
			return nil, fmt.Errorf("%s: %v out of range [%v, %v]", setting.ID, f, setting.Min, setting.Max)
		}
		return f, nil

	case TypeEnum:
		s, ok := raw.(string)
		if !ok {
			return nil, fmt.Errorf("%s: expected string enum, got %T", setting.ID, raw)
		}
		for _, opt := range setting.Options {
			if opt == s {
				return s, nil
			}
		}
		return nil, fmt.Errorf("%s: %q not in options %v", setting.ID, s, setting.Options)

	case TypeString:
		s, ok := raw.(string)
		if !ok {
			return nil, fmt.Errorf("%s: expected string, got %T", setting.ID, raw)
		}
		return s, nil
	}
	return raw, nil
}
