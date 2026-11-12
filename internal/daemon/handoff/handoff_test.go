//go:build !windows

package handoff

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func sample() File {
	return File{
		Format: Format, PID: os.Getpid(), UID: os.Getuid(), Socket: "/tmp/x.sock",
		ListenerFD: 3, LockFD: 4,
		Sessions: []Session{{ID: "s1", PID: 42, FD: 7, Cols: 80, Rows: 24, Screen: []byte("\x1b[Hhi")}},
	}
}

func TestWriteReadRoundTrip(t *testing.T) {
	path := Path(t.TempDir(), os.Getpid())
	if err := Write(path, sample()); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, err %v; want 0600", fi.Mode().Perm(), err)
	}
	if err := Check(path); err != nil {
		t.Fatalf("Check: %v", err)
	}
	got, err := Read(path, os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Sessions) != 1 || string(got.Sessions[0].Screen) != "\x1b[Hhi" || got.Sessions[0].PID != 42 {
		t.Fatalf("round trip lost data: %+v", got)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("Read must remove the file")
	}
}

func TestWriteRefusesExisting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "h.json")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Write(path, sample()); err == nil {
		t.Fatal("Write over an existing file must fail (O_EXCL)")
	}
}

func TestReadRefusesOtherPID(t *testing.T) {
	path := Path(t.TempDir(), 1)
	f := sample()
	f.PID = os.Getpid() + 1
	if err := Write(path, f); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(path, os.Getpid()); err == nil || !strings.Contains(err.Error(), "pid") {
		t.Fatalf("err = %v, want pid refusal", err)
	}
}

func TestReadRefusesOpenMode(t *testing.T) {
	path := Path(t.TempDir(), os.Getpid())
	if err := Write(path, sample()); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(path, os.Getpid()); err == nil {
		t.Fatal("a 0644 handoff file must be refused")
	}
}

func TestReadRefusesSymlink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.json")
	if err := Write(real, sample()); err != nil {
		t.Fatal(err)
	}
	link := Path(dir, os.Getpid())
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(link, os.Getpid()); err == nil {
		t.Fatal("a symlinked handoff file must be refused")
	}
}

func TestReadRefusesUnknownFormat(t *testing.T) {
	path := Path(t.TempDir(), os.Getpid())
	f := sample()
	f.Format = "rex-handoff/99"
	if err := Write(path, f); err != nil {
		t.Fatal(err)
	}
	if err := Check(path); err == nil || !strings.Contains(err.Error(), "unsupported format") {
		t.Fatalf("Check err = %v", err)
	}
}

func TestInheritClearsAndRestoresCloexec(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	fd := int(r.Fd())
	restore, err := Inherit(fd)
	if err != nil {
		t.Fatal(err)
	}
	if fl, _ := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0); fl&unix.FD_CLOEXEC != 0 {
		t.Fatal("FD_CLOEXEC still set after Inherit")
	}
	restore()
	if fl, _ := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0); fl&unix.FD_CLOEXEC == 0 {
		t.Fatal("FD_CLOEXEC not restored")
	}
	if _, err := Inherit(-1); err == nil {
		t.Fatal("negative fd must fail")
	}
}

func TestBuildID(t *testing.T) {
	if id := BuildID(); len(id) != 12 {
		t.Fatalf("BuildID = %q", id)
	}
}
