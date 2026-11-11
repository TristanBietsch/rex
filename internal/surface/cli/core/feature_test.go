package core

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRequireFeature(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	require.NoError(t, RequireFeature("spawn_skills"), "no config: the default is on")

	cfg := filepath.Join(home, ".config", "rex", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(cfg), 0o700))
	require.NoError(t, os.WriteFile(cfg, []byte("spawn_skills: false\n"), 0o600))
	err := RequireFeature("spawn_skills")
	require.Error(t, err)
	info := Describe(err)
	require.Equal(t, ExitInvalidArgs, info.Code)
	require.Equal(t, "off: enable in Settings → Features (rex config set spawn_skills true)", info.Msg)
	require.Equal(t, "rex config set spawn_skills true", info.Hint)
}

func TestStringList(t *testing.T) {
	fs := flag.NewFlagSet("x", flag.ContinueOnError)
	var l StringList
	fs.Var(&l, "skill", "")
	pos, err := ParseArgs(fs, []string{"-skill", "a", "text", "--skill=b", "-skill", "a"})
	require.NoError(t, err)
	require.Equal(t, []string{"text"}, pos)
	require.Equal(t, StringList{"a", "b", "a"}, l)
	require.Equal(t, "a,b,a", l.String())

	var nilList *StringList
	require.Empty(t, nilList.String())

	fs = flag.NewFlagSet("x", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var e StringList
	fs.Var(&e, "skill", "")
	_, err = ParseArgs(fs, []string{"-skill", ""})
	require.Error(t, err)
}
