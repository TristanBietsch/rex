package audio

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ClaimTTL is how long a sound claim holds. Every board gets a daemon event
// within milliseconds of the others, so a few seconds cover a slow board;
// a claim left by a board that crashed expires after it.
const ClaimTTL = 10 * time.Second

// claimNameLen is the length of a claim file name: 128 bits of SHA-256, hex.
const claimNameLen = 32

// Claims makes a session sound play once per user, however many boards are
// open. Each board receives every daemon event and decides on its own that a
// sound is due; before it plays, it claims the event's key in a shared
// owner-only folder (O_CREATE|O_EXCL of a small file named by the key's
// hash). The first board to create the file plays; the others stay silent.
// Claims older than ClaimTTL are stale: they no longer silence anyone and
// are pruned.
//
// Any problem with the folder (a symlink, another user's, open to group or
// others, not creatable) turns dedupe off for that claim: Claim reports a
// win and the sound plays, as it did before dedupe. It never panics.
//
// Safe for concurrent use. A nil *Claims always wins.
type Claims struct {
	dir string
	ttl time.Duration
	now func() time.Time

	mu        sync.Mutex
	lastPrune time.Time
	warned    bool
}

// NewClaims returns Claims kept in dir (paths.SoundClaimsDir). The folder is
// created (0700) on the first claim.
func NewClaims(dir string) *Claims {
	return &Claims{dir: dir, ttl: ClaimTTL, now: time.Now}
}

// Claim reports whether this process should play the sound for key: true
// for the first claim of key within ClaimTTL, false when another board (or
// this one) already claimed it.
func (c *Claims) Claim(key string) bool {
	if c == nil || c.dir == "" {
		return true
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	if err := c.ensureDir(); err != nil {
		c.warn("sound claim folder refused; every board plays", err)
		return true
	}
	c.prune(now)
	path := filepath.Join(c.dir, claimName(key))
	for range 2 {
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|oNoFollow, 0o600) //nolint:gosec // G304: name is a hash inside the checked claim folder.
		if err == nil {
			_ = f.Close()
			_ = os.Chtimes(path, now, now) // the claim time is its mtime
			return true
		}
		if !errors.Is(err, fs.ErrExist) {
			c.warn("sound claim failed; playing", err)
			return true
		}
		fi, err := os.Lstat(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue // pruned by another board in between: try again
		}
		if err != nil {
			c.warn("sound claim unreadable; playing", err)
			return true
		}
		if !fi.Mode().IsRegular() {
			c.warn("sound claim is not a regular file; playing", fmt.Errorf("%s: %s", path, fi.Mode().Type()))
			return true
		}
		if now.Sub(fi.ModTime()) < c.ttl {
			return false
		}
		// Stale: the same key long ago, or a board that died. Remove it
		// and claim afresh.
		_ = os.Remove(path)
	}
	return true
}

// ensureDir creates the claim folder (0700) and checks it is a real folder
// that only this user can write.
func (c *Claims) ensureDir() error {
	if err := os.MkdirAll(filepath.Dir(c.dir), 0o700); err != nil {
		return err
	}
	if err := os.Mkdir(c.dir, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	fi, err := os.Lstat(c.dir)
	if err != nil {
		return err
	}
	if fi.Mode()&fs.ModeSymlink != 0 {
		return fmt.Errorf("%s is a symlink", c.dir)
	}
	if !fi.IsDir() {
		return fmt.Errorf("%s is not a folder", c.dir)
	}
	return checkClaimDir(fi)
}

// prune removes stale claims, at most once per TTL. Only files named like a
// claim are touched.
func (c *Claims) prune(now time.Time) {
	if !c.lastPrune.IsZero() && now.Sub(c.lastPrune) < c.ttl {
		return
	}
	c.lastPrune = now
	ents, err := os.ReadDir(c.dir)
	if err != nil {
		return
	}
	for _, e := range ents {
		if !isClaimName(e.Name()) {
			continue
		}
		p := filepath.Join(c.dir, e.Name())
		fi, err := os.Lstat(p)
		if err != nil || !fi.Mode().IsRegular() || now.Sub(fi.ModTime()) < c.ttl {
			continue
		}
		_ = os.Remove(p)
	}
}

// warn logs the first problem only, so a refused folder does not flood the
// log with one line per sound.
func (c *Claims) warn(msg string, err error) {
	if c.warned {
		slog.Debug("audio: "+msg, "dir", c.dir, "err", err)
		return
	}
	c.warned = true
	slog.Warn("audio: "+msg, "dir", c.dir, "err", err)
}

// claimName is the file name for key: a fixed-length hash, so nothing from
// the key reaches the file system as a path.
func claimName(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:claimNameLen/2])
}

// isClaimName reports whether name has the shape claimName produces.
func isClaimName(name string) bool {
	if len(name) != claimNameLen {
		return false
	}
	for _, r := range name {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}
