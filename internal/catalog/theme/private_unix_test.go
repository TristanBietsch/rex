//go:build !windows

package theme

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckPrivate_ForeignOwner(t *testing.T) {
	fi, err := os.Stat(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, checkPrivate(fi, os.Geteuid(), ""))
	err = checkPrivate(fi, os.Geteuid()+1, "")
	require.ErrorContains(t, err, "owned by another user")
}
