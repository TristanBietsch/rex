package snapshot

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// A slow git --version must not be charged to the capture limit: it is
// classified as too old, not as a failed capture, and nothing is created.
func TestCaptureWithinSlowGitProbeIsNotChargedToLimit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs a shell script as git")
	}
	bin := t.TempDir()
	//nolint:gosec // a fake git must be executable
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\nsleep 1\necho 'git version 2.30.2'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	s := newStore(t)
	_, err := s.CaptureWithin(context.Background(), t.TempDir(), "s1", PhaseStart, 200*time.Millisecond)
	if !errors.Is(err, ErrGitTooOld) {
		t.Fatalf("err = %v, want ErrGitTooOld", err)
	}
	if _, err := os.Stat(s.Root()); err == nil {
		t.Error("a store was created")
	}
}

// A probe that times out is not cached: the next call probes again.
func TestCheckGitProbeTimeoutIsNotCached(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs a shell script as git")
	}
	old := gitProbeTimeout
	gitProbeTimeout = 200 * time.Millisecond
	t.Cleanup(func() { gitProbeTimeout = old })
	// PATH holds only the fake git below, so the script calls sleep by its
	// full path (a bare sleep is not found and the probe returns at once).
	sleepBin, lookErr := exec.LookPath("sleep")
	if lookErr != nil {
		t.Skip("no sleep binary")
	}
	bin := t.TempDir()
	flag := filepath.Join(bin, "fast")
	script := "#!/bin/sh\nif [ ! -e '" + flag + "' ]; then '" + sleepBin + "' 3; fi\necho 'git version 2.30.2'\n"
	//nolint:gosec // a fake git must be executable
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	err := CheckGit(context.Background())
	if err == nil || errors.Is(err, ErrGitTooOld) {
		t.Fatalf("slow probe: %v, want a timeout error", err)
	}
	if err := os.WriteFile(flag, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	gitProbeTimeout = 30 * time.Second
	if err := CheckGit(context.Background()); !errors.Is(err, ErrGitTooOld) {
		t.Fatalf("retry: %v, want ErrGitTooOld", err)
	}
}
