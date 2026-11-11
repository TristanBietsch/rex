package registry

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuiltinAntigravityResume(t *testing.T) {
	reg, err := Load("")
	require.NoError(t, err)
	agy, ok := reg.Find("antigravity")
	require.True(t, ok)
	require.NotNil(t, agy.Resume)
	const id = "0a1b2c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d"
	require.Equal(t, []string{"--conversation=" + id}, agy.Resume.ArgsFor(id), "one argv element")
	require.Empty(t, agy.Resume.NewArgs, "agy cannot preassign an id")
	require.Empty(t, agy.Resume.NewArgsFor(id))
}
