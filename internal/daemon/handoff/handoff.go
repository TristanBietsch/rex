// Package handoff carries a live daemon's sessions across a re-exec of the
// daemon in place (R20 hot restart). The old image writes a File naming the
// file descriptors it keeps open across execve; the new image, which has the
// same pid, reads it back and adopts them.
//
// The file is the only channel. It lives in the owner-only state dir, is
// created O_EXCL 0600, and Read accepts it only when it is a regular file
// owned by this user, closed to group and other, and names this process's
// pid: a file written by any other process names another pid.
package handoff

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
)

// Format is the handoff file format this build writes and reads.
const Format = "rex-handoff/1"

// maxFileBytes bounds a handoff file read (screens are a few KiB each).
const maxFileBytes = 64 << 20

// Session is one live session handed across the exec.
type Session struct {
	ID string `json:"id"`
	// PID is the agent's process id; it stays this daemon's child.
	PID int `json:"pid"`
	// FD is the PTY master's descriptor number, open across exec.
	FD int `json:"fd"`
	// Cols and Rows are the PTY size.
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
	// Screen is the emulated screen as escape sequences (terminal modes,
	// then a full repaint); replayed into the new image's emulator.
	Screen []byte `json:"screen,omitempty"`
}

// File is the whole handoff.
type File struct {
	Format string `json:"format"`
	// PID is the daemon's pid; only the image exec'd into it may read it.
	PID int `json:"pid"`
	UID int `json:"uid"`
	// Build is the writer's build id (BuildID).
	Build string `json:"build,omitempty"`
	// Socket is the path the listener is bound to.
	Socket     string    `json:"socket"`
	ListenerFD int       `json:"listener_fd"`
	LockFD     int       `json:"lock_fd"`
	Sessions   []Session `json:"sessions"`
}

// Path is the handoff file of the daemon with pid under stateDir.
func Path(stateDir string, pid int) string {
	return filepath.Join(stateDir, "handoff-"+strconv.Itoa(pid)+".json")
}

// Write creates path (it must not exist) with mode 0600 and writes f.
func Write(path string, f File) error {
	b, err := json.Marshal(f)
	if err != nil {
		return fmt.Errorf("handoff: encode: %w", err)
	}
	out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("handoff: create: %w", err)
	}
	_, werr := out.Write(b)
	serr := out.Sync()
	cerr := out.Close()
	if err := errors.Join(werr, serr, cerr); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("handoff: write: %w", err)
	}
	return nil
}

// Check parses path and reports whether this build can take it over. It is
// the new binary's answer to the old daemon's preflight (rex-daemon
// -handoff-check) and does not remove the file.
func Check(path string) error {
	_, err := load(path)
	return err
}

// Read loads path for the daemon with pid wantPID and removes it. It
// refuses a symlink, a non-regular file, a file owned by another user or
// open to group/other, another pid and an unknown format.
func Read(path string, wantPID int) (File, error) {
	f, err := load(path)
	if err != nil {
		return File{}, err
	}
	if rmErr := os.Remove(path); rmErr != nil && !errors.Is(rmErr, fs.ErrNotExist) {
		return File{}, fmt.Errorf("handoff: remove: %w", rmErr)
	}
	if f.PID != wantPID {
		return File{}, fmt.Errorf("handoff: file is for pid %d, not %d", f.PID, wantPID)
	}
	if f.UID != os.Getuid() {
		return File{}, fmt.Errorf("handoff: file is for uid %d", f.UID)
	}
	return f, nil
}

func load(path string) (File, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return File{}, fmt.Errorf("handoff: %w", err)
	}
	if !fi.Mode().IsRegular() {
		return File{}, fmt.Errorf("handoff: %s is not a regular file", path)
	}
	if err := checkOwner(fi); err != nil {
		return File{}, err
	}
	in, err := os.Open(path)
	if err != nil {
		return File{}, fmt.Errorf("handoff: %w", err)
	}
	defer in.Close()
	b, err := io.ReadAll(io.LimitReader(in, maxFileBytes+1))
	if err != nil {
		return File{}, fmt.Errorf("handoff: read: %w", err)
	}
	if len(b) > maxFileBytes {
		return File{}, errors.New("handoff: file too large")
	}
	var f File
	if err := json.Unmarshal(b, &f); err != nil {
		return File{}, fmt.Errorf("handoff: decode: %w", err)
	}
	if f.Format != Format {
		return File{}, fmt.Errorf("handoff: unsupported format %q (this build reads %s)", f.Format, Format)
	}
	return f, nil
}

// BuildID is the first 12 hex digits of the SHA-256 of this executable, ""
// when it cannot be read. Two builds of the same version differ here.
func BuildID() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return FileBuildID(exe)
}

// FileBuildID is BuildID of the binary at path.
func FileBuildID(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
}
