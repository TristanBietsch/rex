package settings

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"gopkg.in/yaml.v3"
)

// Store holds the effective settings: defaults overlaid with values loaded from
// ~/.config/rex/config.yaml.
type Store struct {
	values map[string]any
}

// DefaultPath returns the standard config.yaml location.
func DefaultPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "rex", "config.yaml")
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

// Save writes the current values to path atomically.
func (s *Store) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	b, err := yaml.Marshal(s.values)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return fmt.Errorf("write tmp: %w", err)
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
