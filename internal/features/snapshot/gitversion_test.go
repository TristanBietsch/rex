package snapshot

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func fakeGit(t *testing.T, version string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("needs a shell script as git")
	}
	bin := t.TempDir()
	//nolint:gosec // a fake git must be executable
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\necho 'git version "+version+"'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
}

func TestCheckGitRefusesOldVersions(t *testing.T) {
	fakeGit(t, "2.31.9 (Apple Git-1)")
	if err := CheckGit(context.Background()); !errors.Is(err, ErrGitTooOld) {
		t.Fatalf("2.31: %v", err)
	}
	// A capture refuses too, before touching anything.
	repo := t.TempDir()
	s := newStore(t)
	if _, err := s.Capture(context.Background(), repo, "s1", PhaseStart); !errors.Is(err, ErrGitTooOld) {
		t.Fatalf("capture: %v", err)
	}
	if _, err := os.Stat(s.Root()); err == nil {
		t.Error("a store was created with an old git")
	}
}

func TestCheckGitAcceptsNewVersions(t *testing.T) {
	for _, v := range []string{"2.32.0", "2.56.0", "3.0.1"} {
		fakeGit(t, v)
		if err := CheckGit(context.Background()); err != nil {
			t.Errorf("%s: %v", v, err)
		}
	}
}

func TestCheckGitNoGit(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if err := CheckGit(context.Background()); !errors.Is(err, ErrNoGit) {
		t.Fatalf("err = %v", err)
	}
}
