package session

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseNameArgs(t *testing.T) {
	a, err := parseNameArgs([]string{"ab12", "fix", " the", "bug"})
	require.NoError(t, err)
	require.Equal(t, "ab12", a.sel)
	require.Equal(t, "fix the bug", a.name)
	require.False(t, a.show)

	a, err = parseNameArgs([]string{"ab12"})
	require.NoError(t, err)
	require.True(t, a.show)

	a, err = parseNameArgs([]string{"-clear", "ab12"})
	require.NoError(t, err)
	require.True(t, a.clear)
	require.Equal(t, "", a.name)

	_, err = parseNameArgs([]string{"-clear", "ab12", "x"})
	require.Error(t, err)
	_, err = parseNameArgs(nil)
	require.Error(t, err)
	_, err = parseNameArgs([]string{"ab12", strings.Repeat("x", 65)})
	require.ErrorContains(t, err, "64")
	_, err = parseNameArgs([]string{"ab12", "\x1b"})
	require.ErrorContains(t, err, "-clear", "control-only text normalizes to empty")
}
