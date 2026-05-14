package registry

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoad_BuiltinOnly(t *testing.T) {
	reg, err := Load("")
	require.NoError(t, err)

	echo, ok := reg.Find("echo")
	require.True(t, ok)
	require.Equal(t, "Echo (test)", echo.Name)
	require.Len(t, echo.Models, 3)
}

func TestLoad_UserExtends(t *testing.T) {
	wd, _ := os.Getwd()
	user := filepath.Join(wd, "..", "..", "testdata", "tools-user.yaml")

	reg, err := Load(user)
	require.NoError(t, err)

	echo, _ := reg.Find("echo")
	require.Len(t, echo.Models, 4) // 3 builtin + 1 user-extra

	user2, ok := reg.Find("usertool")
	require.True(t, ok)
	require.Equal(t, "#FF00FF", user2.Color)
}

func TestLoad_BadYAMLFails(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "bad.yaml")
	require.NoError(t, os.WriteFile(tmp, []byte("tools: [oops"), 0o644))
	_, err := Load(tmp)
	require.Error(t, err)
}
