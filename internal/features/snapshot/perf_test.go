package snapshot

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A tree with many individually ignored files (build objects spread over
// many directories, so no parent folder is ignored as a whole) must still
// capture in reasonable time. Skipped with -short.
func TestCapturePerformanceWithManyIgnoredFiles(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: writes ~50k files")
	}
	repo := newRepo(t)
	write(t, repo, ".gitignore", "*.o\n")
	// 10,000 files keeps `make check` quick under the race detector; set
	// REX_SNAPSHOT_PERF=full for the 50,000-file measurement.
	dirs, perDir := 100, 100
	if os.Getenv("REX_SNAPSHOT_PERF") == "full" {
		dirs = 500
	}
	for d := 0; d < dirs; d++ {
		dir := filepath.Join(repo, "src", fmt.Sprintf("d%03d", d))
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "main.c"), []byte("int main(){}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		for f := 0; f < perDir; f++ {
			if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%03d.o", f)), []byte("obj"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	s := newStore(t)
	ctx := context.Background()
	t0 := time.Now()
	first := capture(t, s, repo, "s1", PhaseStart)
	d1 := time.Since(t0)
	write(t, repo, "src/d000/main.c", "int main(){return 1;}\n")
	t0 = time.Now()
	capture(t, s, repo, "s1", PhaseEnd)
	d2 := time.Since(t0)
	t0 = time.Now()
	res, err := s.PlanUndo(ctx, repo, "s1", 1)
	d3 := time.Since(t0)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d ignored files: first capture %v, second capture %v, plan %v (restore %d files)", dirs*perDir, d1, d2, d3, len(res.Restored))
	ch, err := s.Changed(ctx, repo, emptyTree, first)
	if err != nil {
		t.Fatal(err)
	}
	if len(ch) > dirs+5 {
		t.Errorf("ignored files were captured: %d paths in the tree", len(ch))
	}
	if d1 > 60*time.Second || d2 > 30*time.Second {
		t.Errorf("capture too slow: %v / %v", d1, d2)
	}
}
