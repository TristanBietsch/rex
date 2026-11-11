package e2e

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/testutil/testperm"
)

// Each script must run its own copy of rex and rex-daemon, never a hard
// link to the shared build output. Hard links share one vnode, and macOS
// reports one of the vnode's names when Gatekeeper assesses a new
// executable. When that name belonged to a script that had already
// finished and removed its $WORK, syspolicyd could not open the file and
// SIGKILLed the daemon at exec ("Terminating process due to Gatekeeper
// rejection"), which surfaced as `rex-daemon exited early (signal:
// killed)` under the parallel -race run.
func TestCopyBinary_PrivateInode(t *testing.T) {
	src := filepath.Join(t.TempDir(), "rex-daemon")
	testperm.WriteFile(t, src, []byte("#!/bin/sh\nexit 0\n"), 0o700)
	dst := filepath.Join(t.TempDir(), "rex-daemon")

	require.NoError(t, copyBinary(src, dst))

	srcInfo, err := os.Stat(src)
	require.NoError(t, err)
	dstInfo, err := os.Stat(dst)
	require.NoError(t, err)
	require.False(t, os.SameFile(srcInfo, dstInfo), "script binary shares an inode with the build output")
	// Owner-executable only: 0700 on Unix. Windows has no execute bit and
	// reports a writable 0666 file.
	testperm.RequireMode(t, dst, 0o700)
	got, err := os.ReadFile(dst)
	require.NoError(t, err)
	require.Equal(t, "#!/bin/sh\nexit 0\n", string(got))
}
