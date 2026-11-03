package settings

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

// requireValidValues asserts every registered setting holds a value its spec
// accepts: the right Go type, an enum member, and inside Min/Max where coerce
// enforces a range. Callers type-assert these values without checks.
func requireValidValues(t *testing.T, s *Store) {
	t.Helper()
	for _, set := range Registry {
		v := s.Get(set.ID)
		switch set.Type {
		case TypeBool:
			require.IsType(t, false, v, set.ID)
		case TypeString:
			require.IsType(t, "", v, set.ID)
		case TypeEnum:
			str, ok := v.(string)
			require.True(t, ok, "%s: %T is not a string", set.ID, v)
			require.True(t, slices.Contains(set.Options, str), "%s: %q not in %v", set.ID, str, set.Options)
		case TypeInt:
			i, ok := v.(int)
			require.True(t, ok, "%s: %T is not an int", set.ID, v)
			if set.Min != 0 || set.Max != 0 {
				require.True(t, float64(i) >= set.Min && float64(i) <= set.Max,
					"%s: %d outside [%v, %v]", set.ID, i, set.Min, set.Max)
			}
		case TypeFloat:
			fl, ok := v.(float64)
			require.True(t, ok, "%s: %T is not a float64", set.ID, v)
			if set.Max > set.Min {
				// Written as "inside" rather than "not outside" so NaN fails.
				require.True(t, fl >= set.Min && fl <= set.Max,
					"%s: %v outside [%v, %v]", set.ID, fl, set.Min, set.Max)
			}
		}
	}
}

// FuzzStoreLoad feeds arbitrary bytes to Store.Load as config.yaml. The TUI,
// the CLI and the daemon all load this file, so it must never panic, and the
// store must end up holding only values the registry allows: Load promises to
// skip invalid entries, and a failed parse must leave the defaults intact.
func FuzzStoreLoad(f *testing.F) {
	for _, s := range []string{
		"spinner: pulse\nmaster_volume: 0.5\n",
		"spinner: blocks\n",
		"spinner: spiral\n",
		"master_volume: 1.5\n",
		"mouse_enabled: \"false\"\n",
		"max_concurrent_sessions: 16\nretire_after_hours: 72\n",
		"lua_config_path: /tmp/foo\n",
		"unknown_key: 1\n",
		"[oops",
		"",
	} {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		require.NoError(t, os.WriteFile(path, data, 0o600))

		s := NewStore()
		if err := s.Load(path); err != nil {
			require.Equal(t, NewStore().Snapshot(), s.Snapshot(), "failed Load changed values")
			return
		}
		requireValidValues(t, s)
	})
}
