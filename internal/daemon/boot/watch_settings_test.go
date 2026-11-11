package boot

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/features/summarizer"
)

// runWatch starts WatchSettings and stops it (and waits) when the test ends.
func runWatch(t *testing.T, path string, apply ...func(*settings.Store)) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); WatchSettings(ctx, path, 10*time.Millisecond, apply...) }()
	t.Cleanup(func() { cancel(); <-done })
}

// write replaces path and gives it a distinct mtime, so a coarse filesystem
// clock cannot hide the change.
func write(t *testing.T, path, body string, mtime time.Time) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	require.NoError(t, os.Chtimes(path, mtime, mtime))
}

type recorder struct {
	mu   sync.Mutex
	vals []string
}

func (r *recorder) apply(key string) func(*settings.Store) {
	return func(s *settings.Store) {
		r.mu.Lock()
		r.vals = append(r.vals, s.String(key))
		r.mu.Unlock()
	}
}

func (r *recorder) got() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.vals...)
}

func TestWatchSettings_AppliesOnChangeToEveryApplier(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	t0 := time.Now().Add(-time.Hour)
	write(t, path, "summary_detail: short\n", t0)
	var a, b recorder
	runWatch(t, path, a.apply("summary_detail"), b.apply("summary_detail"))

	time.Sleep(60 * time.Millisecond)
	require.Empty(t, a.got(), "the state at start counts as applied")

	write(t, path, "summary_detail: long\n", t0.Add(time.Minute))
	require.Eventually(t, func() bool { return len(a.got()) == 1 && len(b.got()) == 1 }, 2*time.Second, 5*time.Millisecond)
	require.Equal(t, []string{"long"}, a.got())
	require.Equal(t, []string{"long"}, b.got())

	time.Sleep(60 * time.Millisecond)
	require.Len(t, a.got(), 1, "no change, no apply")
}

func TestWatchSettings_CreatedAndRemovedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	var r recorder
	runWatch(t, path, r.apply("summary_detail"))

	write(t, path, "summary_detail: long\n", time.Now())
	require.Eventually(t, func() bool { return len(r.got()) == 1 }, 2*time.Second, 5*time.Millisecond)
	require.Equal(t, "long", r.got()[0])

	require.NoError(t, os.Remove(path))
	require.Eventually(t, func() bool { return len(r.got()) == 2 }, 2*time.Second, 5*time.Millisecond)
	require.Equal(t, "medium", r.got()[1], "a removed file applies the defaults")
}

func TestWatchSettings_NoAppliersOrPathReturnsAtOnce(t *testing.T) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		WatchSettings(context.Background(), "", time.Millisecond, func(*settings.Store) {})
		WatchSettings(context.Background(), "x", time.Millisecond)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("WatchSettings blocked with nothing to do")
	}
}

func TestSummaryDetailApplier_AppliesOnMtimeChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	t0 := time.Now().Add(-time.Hour)
	write(t, path, "summary_enabled: true\n", t0)
	w := summarizer.New(summarizer.Defaults(), state.NewStore(), func(string, int) []byte { return nil })
	require.Equal(t, summarizer.DetailMedium, w.Detail())
	runWatch(t, path, SummaryDetailApplier(w))

	write(t, path, "summary_detail: long\n", t0.Add(time.Minute))
	require.Eventually(t, func() bool { return w.Detail() == summarizer.DetailLong }, 2*time.Second, 5*time.Millisecond)

	write(t, path, "summary_detail: short\n", t0.Add(2*time.Minute))
	require.Eventually(t, func() bool { return w.Detail() == summarizer.DetailShort }, 2*time.Second, 5*time.Millisecond)

	// Dropping the key (or an invalid value) falls back to medium.
	write(t, path, "summary_detail: huge\n", t0.Add(3*time.Minute))
	require.Eventually(t, func() bool { return w.Detail() == summarizer.DetailMedium }, 2*time.Second, 5*time.Millisecond)
}
