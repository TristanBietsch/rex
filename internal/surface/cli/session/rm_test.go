package session

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// requireDeleted waits for the fire-and-forget Delete to reach the daemon.
func requireDeleted(t *testing.T, d *pruneDaemon) {
	t.Helper()
	require.Eventually(t, func() bool {
		return len(d.Deleted()) == 1 && d.Deleted()[0] == pruneSessions[0].ID
	}, 2*time.Second, 10*time.Millisecond)
}

func TestRunRm_NonTTYNeedsForce(t *testing.T) {
	d := startPruneDaemon(t, true)
	_, errOut := withPruneIO(t, "y\n", false)
	err := RunRm([]string{"-socket", d.sock, "1111"})
	require.Equal(t, 6, exitCode(t, err))
	require.ErrorContains(t, err, "-force")
	require.Empty(t, d.Deleted())
	require.Empty(t, errOut.String(), "no prompt without a terminal")
}

func TestRunRm_ForceAliases(t *testing.T) {
	for _, flag := range []string{"-force", "-yes", "-y"} {
		t.Run(flag, func(t *testing.T) {
			d := startPruneDaemon(t, true)
			withPruneIO(t, "", false)
			require.NoError(t, RunRm([]string{"-socket", d.sock, flag, "1111"}))
			requireDeleted(t, d)
		})
	}
	t.Run("flag after selector", func(t *testing.T) {
		d := startPruneDaemon(t, true)
		withPruneIO(t, "", false)
		require.NoError(t, RunRm([]string{"-socket", d.sock, "1111", "-force"}))
		requireDeleted(t, d)
	})
}

func TestRunRm_DeclineExits6(t *testing.T) {
	for _, in := range []string{"n\n", "\n", ""} {
		d := startPruneDaemon(t, true)
		_, errOut := withPruneIO(t, in, true)
		err := RunRm([]string{"-socket", d.sock, "1111"})
		require.Equal(t, 6, exitCode(t, err), "answer %q", in)
		require.Empty(t, d.Deleted())
		require.Contains(t, errOut.String(), "delete 1111 (done-one)? [y/N]")
	}
}

func TestRunRm_TTYConfirmDeletes(t *testing.T) {
	for _, in := range []string{"y\n", "Y\n", "yes\n"} {
		d := startPruneDaemon(t, true)
		withPruneIO(t, in, true)
		require.NoError(t, RunRm([]string{"-socket", d.sock, "1111"}), in)
		requireDeleted(t, d)
	}
}

// A closed stdin used to be a nil dereference in rm and new.
func TestPromptFromStdin_ClosedStdin(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "stdin")
	require.NoError(t, err)
	require.NoError(t, f.Close())
	got, err := promptFromStdin(f)
	require.NoError(t, err)
	require.Empty(t, got)

	got, err = promptFromStdin(nil)
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestPromptFromStdin_Pipe(t *testing.T) {
	r, w, err := os.Pipe()
	require.NoError(t, err)
	defer r.Close()
	_, err = w.WriteString("do the thing\n")
	require.NoError(t, err)
	require.NoError(t, w.Close())
	got, err := promptFromStdin(r)
	require.NoError(t, err)
	require.Equal(t, "do the thing\n", got)
}
