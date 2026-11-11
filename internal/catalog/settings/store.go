package settings

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"gopkg.in/yaml.v3"

	"github.com/tristanbietsch/rex/internal/runtime/paths"
)

// Store holds the effective settings: defaults overlaid with values loaded from
// config.yaml (paths.ConfigFile).
type Store struct {
	values map[string]any
}

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
	s := &Store{values: make(map[string]any, len(Registry))}
	for _, r := range Registry {
		s.values[r.ID] = r.Default
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
			continue // tolerate unknown keys for forward compat
		}
		coerced, err := coerce(setting, v)
		if err != nil {
			continue // skip invalid entries silently
		}
		s.values[k] = coerced
	}
	return nil
}

// Save writes the current values to path atomically (temp file, then
// rename). A missing directory is created 0700. A new file is 0600; an
// existing one keeps its permission bits, so Save never widens a private
// config.yaml and never undoes a mode its owner chose.
func (s *Store) Save(path string) error {
	if path == "" {
		return errNoPath
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	b, err := yaml.Marshal(s.values)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	mode := os.FileMode(0o600)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}
	// WriteFile keeps the mode of a leftover tmp and is subject to the
	// umask, so set the final mode explicitly.
	if err := os.Chmod(tmp, mode); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("chmod tmp: %w", err)
	}
	return os.Rename(tmp, path)
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

// Set validates the new value against the Setting spec and stores it.
func (s *Store) Set(id string, raw any) error {
	setting, ok := Find(id)
	if !ok {
		return fmt.Errorf("unknown setting %q", id)
	}
	if setting.ReadOnly {
		return fmt.Errorf("setting %q is read-only", id)
	}
	v, err := coerce(setting, raw)
	if err != nil {
		return err
	}
	if str, ok := v.(string); ok && setting.Normalize != nil {
		if str, err = setting.Normalize(str); err != nil {
			return fmt.Errorf("%s: %w", id, err)
		}
		v = str
	}
	s.values[id] = v
	return nil
}

// Reset restores the setting to its default.
func (s *Store) Reset(id string) error {
	setting, ok := Find(id)
	if !ok {
		return fmt.Errorf("unknown setting %q", id)
	}
	s.values[id] = setting.Default
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
