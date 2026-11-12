package settings

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWithDefaults(t *testing.T) {
	s := NewStore()
	require.NoError(t, s.Set("skip_permissions", true))
	require.NoError(t, s.Set("summary_detail", "long"))
	c := s.WithDefaults("skip_permissions")
	require.Equal(t, false, c.Get("skip_permissions"))
	require.Equal(t, SourceDefault, c.Source("skip_permissions"))
	require.Equal(t, "long", c.String("summary_detail"), "other values are kept")
	require.Equal(t, true, s.Get("skip_permissions"), "the original is untouched")
	require.Panics(t, func() { s.WithDefaults("nope") })
}
