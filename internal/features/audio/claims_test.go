package audio

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// claimsAt returns Claims on dir with a settable clock.
func claimsAt(dir string, now *time.Time) *Claims {
	c := NewClaims(dir)
	c.now = func() time.Time { return *now }
	return c
}

// Two boards sharing a claim dir: the first claim of a key wins, the second
// loses; a different key is independent.
func TestClaims_OneWinnerPerKey(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sound-claims")
	now := time.Now()
	a, b := claimsAt(dir, &now), claimsAt(dir, &now)
	require.True(t, a.Claim("s1|attention|x"))
	require.False(t, b.Claim("s1|attention|x"))
	require.False(t, a.Claim("s1|attention|x"), "the winner itself never plays a key twice")
	require.True(t, b.Claim("s1|resume|y"))

	fi, err := os.Lstat(dir)
	require.NoError(t, err)
	require.True(t, fi.IsDir())
	if runtime.GOOS != "windows" {
		require.Equal(t, os.FileMode(0o700), fi.Mode().Perm())
		ents, err := os.ReadDir(dir)
		require.NoError(t, err)
		require.Len(t, ents, 2)
		for _, e := range ents {
			info, err := e.Info()
			require.NoError(t, err)
			require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
		}
	}
}

// Many boards racing for one key: exactly one wins.
func TestClaims_RaceOneWinner(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sound-claims")
	var wins atomic.Int32
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			if NewClaims(dir).Claim("s1|done|z") {
				wins.Add(1)
			}
		})
	}
	wg.Wait()
	require.Equal(t, int32(1), wins.Load())
}

// A claim older than the TTL is stale (a board that crashed after claiming,
// or the same key long ago): the key plays again.
func TestClaims_Expire(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sound-claims")
	now := time.Now()
	a, b := claimsAt(dir, &now), claimsAt(dir, &now)
	require.True(t, a.Claim("k"))
	now = now.Add(ClaimTTL - time.Second)
	require.False(t, b.Claim("k"))
	now = now.Add(2 * time.Second)
	require.True(t, b.Claim("k"), "an expired claim plays again")
}

// A crashed board's leftover claim file expires and is pruned.
func TestClaims_CrashLeftoverExpiresAndPrunes(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sound-claims")
	now := time.Now()
	crashed := claimsAt(dir, &now)
	require.True(t, crashed.Claim("left-behind"))
	require.True(t, crashed.Claim("other"))
	// The process dies; a new board starts well past the TTL.
	now = now.Add(ClaimTTL + time.Minute)
	fresh := claimsAt(dir, &now)
	require.True(t, fresh.Claim("left-behind"))
	// Pruning removed the stale "other" claim; only the new claim is left.
	ents, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, ents, 1)
	// A stray file that is not a claim is never deleted.
	stray := filepath.Join(dir, "README")
	require.NoError(t, os.WriteFile(stray, nil, 0o600))
	require.NoError(t, os.Chtimes(stray, now.Add(-time.Hour), now.Add(-time.Hour)))
	now = now.Add(ClaimTTL + time.Minute)
	require.True(t, fresh.Claim("again"))
	_, err = os.Lstat(stray)
	require.NoError(t, err)
}

// A claim dir that is a symlink is refused: every claim wins (the sound
// plays, as before dedupe), nothing is written through the link.
func TestClaims_SymlinkDirRefused(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "elsewhere")
	require.NoError(t, os.Mkdir(target, 0o700))
	dir := filepath.Join(root, "sound-claims")
	if err := os.Symlink(target, dir); err != nil {
		t.Skipf("symlink: %v", err)
	}
	c := NewClaims(dir)
	require.True(t, c.Claim("k"))
	require.True(t, c.Claim("k"), "refused dir: no dedupe, always plays")
	ents, err := os.ReadDir(target)
	require.NoError(t, err)
	require.Empty(t, ents)
}

// A claim file that is a symlink is not followed and does not silence the
// sound.
func TestClaims_SymlinkClaimFile(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "sound-claims")
	require.NoError(t, os.Mkdir(dir, 0o700))
	victim := filepath.Join(root, "victim")
	require.NoError(t, os.WriteFile(victim, []byte("keep"), 0o600))
	if err := os.Symlink(victim, filepath.Join(dir, claimName("k"))); err != nil {
		t.Skipf("symlink: %v", err)
	}
	c := NewClaims(dir)
	require.True(t, c.Claim("k"))
	b, err := os.ReadFile(victim)
	require.NoError(t, err)
	require.Equal(t, "keep", string(b))
}

// A claim dir others can write is refused (no dedupe, the sound plays).
func TestClaims_LooseDirRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no Unix permission bits")
	}
	dir := filepath.Join(t.TempDir(), "sound-claims")
	require.NoError(t, os.Mkdir(dir, 0o700))
	require.NoError(t, os.Chmod(dir, 0o777)) //nolint:gosec // G302: the hostile dir under test.
	c := NewClaims(dir)
	require.True(t, c.Claim("k"))
	require.True(t, c.Claim("k"))
	ents, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Empty(t, ents)
}

// A claim path that is a regular file, or a dir that cannot be created,
// never crashes: claims win.
func TestClaims_UnusableDir(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "sound-claims")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	require.True(t, NewClaims(file).Claim("k"))
	require.True(t, NewClaims(filepath.Join(file, "sub")).Claim("k"))
	require.True(t, NewClaims("").Claim("k"))
	var nilClaims *Claims
	require.True(t, nilClaims.Claim("k"))
}

// Keys hash to fixed-length file names: no path element from a key reaches
// the file system.
func TestClaims_NameIsHash(t *testing.T) {
	n := claimName("../../etc/passwd|attention")
	require.Len(t, n, claimNameLen)
	require.True(t, isClaimName(n))
	require.NotContains(t, n, "/")
	require.NotEqual(t, n, claimName("other"))
}
