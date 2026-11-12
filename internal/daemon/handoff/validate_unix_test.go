//go:build !windows

package handoff

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// The new image takes over only a handoff file it can trust and parse:
// each malformed or missing file is refused with an error naming why.
func TestCheckRefusesInvalidFiles(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, b []byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, b, 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	huge := filepath.Join(dir, "huge.json")
	hf, err := os.OpenFile(huge, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := hf.Truncate(maxFileBytes + 1); err != nil { // sparse: no disk used
		t.Fatal(err)
	}
	_ = hf.Close()
	sub := filepath.Join(dir, "a-dir")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name, path, want string
	}{
		{"missing", filepath.Join(dir, "nope.json"), "no such file"},
		{"directory", sub, "not a regular file"},
		{"not json", write("garbage.json", []byte("{not json")), "decode"},
		{"empty", write("empty.json", nil), "decode"},
		{"no format", write("noformat.json", []byte(`{"pid":1}`)), "unsupported format"},
		{"too large", huge, "too large"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := Check(tc.path)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Check(%s) err = %v, want %q", tc.name, err, tc.want)
			}
			if _, err := Read(tc.path, os.Getpid()); err == nil {
				t.Fatalf("Read(%s) accepted it", tc.name)
			}
		})
	}
}

// A file naming another uid is refused, and like any file Read has parsed
// it is consumed: a refused handoff is never retried.
func TestReadRefusesOtherUIDAndConsumesTheFile(t *testing.T) {
	path := Path(t.TempDir(), os.Getpid())
	f := sample()
	f.UID = os.Getuid() + 1
	if err := Write(path, f); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(path, os.Getpid()); err == nil || !strings.Contains(err.Error(), "uid") {
		t.Fatalf("err = %v, want uid refusal", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("refused file still there: %v", err)
	}
}

// Inherit is all or nothing: when one fd fails, the ones already made
// inheritable get FD_CLOEXEC back, so a later fork leaks nothing.
func TestInheritRollsBackOnABadFD(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	fd := int(r.Fd())
	const closed = 1 << 20 // far above any open descriptor: EBADF
	restore, err := Inherit(fd, closed)
	if err == nil || !strings.Contains(err.Error(), "fd 1048576") {
		t.Fatalf("err = %v, want the bad fd named", err)
	}
	restore() // a no-op: nothing is left to restore
	if fl, _ := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0); fl&unix.FD_CLOEXEC == 0 {
		t.Fatal("FD_CLOEXEC not restored on the fd before the bad one")
	}
}

// KillTree kills a handed-over agent's whole process group; a pid of 0
// (no process) is ignored.
func TestKillTreeKillsTheGroup(t *testing.T) {
	KillTree(0)
	cmd := exec.Command("sh", "-c", "sleep 30 & wait")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	KillTree(cmd.Process.Pid)
	select {
	case err := <-done:
		var ee *exec.ExitError
		if !errors.As(err, &ee) || ee.Sys().(syscall.WaitStatus).Signal() != syscall.SIGKILL {
			t.Fatalf("wait = %v, want killed by SIGKILL", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("process not killed")
	}
	if err := syscall.Kill(-cmd.Process.Pid, 0); err == nil {
		t.Fatal("the group (the background sleep) is still alive")
	}
}
