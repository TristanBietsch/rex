package version

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestFileBuild(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bin")
	data := []byte("rex build fixture")
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	if got, want := FileBuild(p), hex.EncodeToString(sum[:])[:12]; got != want {
		t.Fatalf("FileBuild = %q, want %q", got, want)
	}
	if got := FileBuild(filepath.Join(t.TempDir(), "missing")); got != "" {
		t.Fatalf("FileBuild(missing) = %q, want empty", got)
	}
	if got := FileBuild(t.TempDir()); got != "" {
		t.Fatalf("FileBuild(dir) = %q, want empty", got)
	}
}

func TestBuild(t *testing.T) {
	b := Build()
	if len(b) != 12 {
		t.Fatalf("Build() = %q, want 12 hex digits", b)
	}
	if Build() != b {
		t.Fatal("Build() changed between calls")
	}
}
