package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// errWouldBlock is tryLockFile finding the lock held.
var errWouldBlock = errors.New("lock held")

// trustLockWait is how long a trust write waits for another rex process to
// release the trust file's lock. Tests shorten it.
var trustLockWait = 5 * time.Second

// lockTrustFile takes the cross-process lock that guards the
// read-modify-write of the trust file at path: an exclusive lock on
// path+".lock", created 0600 next to it, never through a symlink. Without
// it `rex ws trust` and `rex ws trust -rm` running at once could each read
// the old file and the later rename would drop the other's change (a lost
// revoke). It waits up to trustLockWait, then fails; the returned func
// releases the lock.
func lockTrustFile(path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	lockPath := path + ".lock"
	f, err := os.OpenFile(lockPath, os.O_RDWR|os.O_CREATE|oNoFollow|oNonBlock, 0o600)
	if err != nil {
		if isLinkOpenErr(err) {
			return nil, fmt.Errorf("%w: %s is a symlink", ErrUnsafe, lockPath)
		}
		return nil, err
	}
	if fi, err := f.Stat(); err != nil || !fi.Mode().IsRegular() {
		_ = f.Close()
		return nil, fmt.Errorf("%w: %s is not a regular file", ErrUnsafe, lockPath)
	}
	deadline := time.Now().Add(trustLockWait)
	for {
		err := tryLockFile(f)
		if err == nil {
			return func() {
				_ = unlockFile(f)
				_ = f.Close()
			}, nil
		}
		if !errors.Is(err, errWouldBlock) || time.Now().After(deadline) {
			_ = f.Close()
			if errors.Is(err, errWouldBlock) {
				return nil, fmt.Errorf("workspace: %s is locked by another rex (waited %s); try again", path, trustLockWait)
			}
			return nil, err
		}
		time.Sleep(10 * time.Millisecond)
	}
}
