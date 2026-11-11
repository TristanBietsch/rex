package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// flipCase swaps the case of every ASCII letter in s.
func flipCase(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r - 32
		case r >= 'A' && r <= 'Z':
			return r + 32
		}
		return r
	}, s)
}

// On a case-insensitive filesystem a case variant of home is home.
func TestTrustDir_RefusesHomeByIdentity(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "Home")
	require.NoError(t, os.MkdirAll(home, 0o700))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	variant := filepath.Join(base, flipCase("Home"))
	if _, err := os.Stat(variant); err != nil {
		t.Skip("case-sensitive filesystem")
	}
	_, err := TrustDir(variant)
	require.ErrorContains(t, err, "home folder")
	_, _, err = AppendTrustDir("", variant)
	require.Error(t, err)
}

// The case variant of a folder that holds home is that folder.
func TestTrustDir_RefusesAncestorsOfHomeByIdentity(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "Users", "Me")
	require.NoError(t, os.MkdirAll(home, 0o700))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	variant := filepath.Join(base, flipCase("Users"))
	if _, err := os.Stat(variant); err != nil {
		t.Skip("case-sensitive filesystem")
	}
	_, err := TrustDir(variant)
	require.ErrorContains(t, err, "holds your home folder")
	_, err = TrustDir(filepath.Join(variant, "Me"))
	require.ErrorContains(t, err, "home folder")
}

// A folder that holds home trusts everything in it: refused, by whole path
// elements, not by string prefix.
func TestTrustDir_RefusesAncestorsOfHome(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "users", "me")
	require.NoError(t, os.MkdirAll(home, 0o700))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	sibling := filepath.Join(base, "users2")
	prefixOfHome := filepath.Join(base, "users", "m")
	require.NoError(t, os.MkdirAll(sibling, 0o700))
	require.NoError(t, os.MkdirAll(prefixOfHome, 0o700))

	for name, dir := range map[string]string{
		"parent":       filepath.Join(base, "users"),
		"grandparent":  base,
		"parent slash": filepath.Join(base, "users") + string(filepath.Separator),
	} {
		_, err := TrustDir(dir)
		require.ErrorContains(t, err, "holds your home folder", name)
		_, _, err = AppendTrustDir("", dir)
		require.Error(t, err, name)
	}
	link := filepath.Join(t.TempDir(), "link")
	require.NoError(t, os.Symlink(filepath.Join(base, "users"), link))
	_, err := TrustDir(link)
	require.ErrorContains(t, err, "holds your home folder", "through a symlink")

	for name, dir := range map[string]string{"sibling with a shared prefix": sibling, "prefix of home's name": prefixOfHome} {
		_, err := TrustDir(dir)
		require.NoError(t, err, name)
	}
}
