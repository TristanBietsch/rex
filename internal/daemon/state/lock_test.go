package state

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAcquireLock_SecondAttemptFailsWithHolderPID(t *testing.T) {
	dir := t.TempDir()
	l, err := AcquireLock(dir)
	require.NoError(t, err)

	b, err := os.ReadFile(filepath.Join(dir, LockFileName))
	require.NoError(t, err)
	require.Equal(t, strconv.Itoa(os.Getpid())+"\n", string(b), "the holder's pid is recorded")

	_, err = AcquireLock(dir)
	require.ErrorIs(t, err, ErrLocked)
	var held *LockedError
	require.ErrorAs(t, err, &held)
	require.Equal(t, os.Getpid(), held.PID)
	require.Equal(t, filepath.Join(dir, LockFileName), held.Path)
	require.Contains(t, err.Error(), strconv.Itoa(os.Getpid()))

	require.NoError(t, l.Release())
	again, err := AcquireLock(dir)
	require.NoError(t, err, "a released lock can be taken again")
	require.NoError(t, again.Release())
}

func TestAcquireLock_StalePIDIsOverwritten(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, LockFileName), []byte("99999999 leftover from a crash\n"), 0o600))
	l, err := AcquireLock(dir)
	require.NoError(t, err, "an unlocked file with a stale pid does not block")
	t.Cleanup(func() { require.NoError(t, l.Release()) })
	b, err := os.ReadFile(filepath.Join(dir, LockFileName))
	require.NoError(t, err)
	require.Equal(t, strconv.Itoa(os.Getpid())+"\n", string(b))
}

func TestAcquireLock_MissingDir(t *testing.T) {
	_, err := AcquireLock(filepath.Join(t.TempDir(), "nope"))
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrLocked)
}

func TestLock_ReleaseTwiceErrors(t *testing.T) {
	l, err := AcquireLock(t.TempDir())
	require.NoError(t, err)
	require.Equal(t, LockFileName, filepath.Base(l.Path()))
	require.NoError(t, l.Release())
	require.Error(t, l.Release(), "the file is already closed")
}

func TestReadLockPID(t *testing.T) {
	for in, want := range map[string]int{
		"123\n": 123, "  77  \n": 77, "42 extra\n": 42, "": 0, "abc\n": 0, "-5\n": 0, "0\n": 0,
	} {
		f, err := os.CreateTemp(t.TempDir(), "pid")
		require.NoError(t, err)
		_, err = f.WriteString(in)
		require.NoError(t, err)
		require.Equal(t, want, readLockPID(f), "input %q", in)
		require.NoError(t, f.Close())
	}
}

func TestLockedError_UnknownPID(t *testing.T) {
	err := &LockedError{Path: "/x/daemon.lock"}
	require.Contains(t, err.Error(), "/x/daemon.lock")
	require.Contains(t, err.Error(), "unknown pid")
}
