package boot

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/daemon/server"
	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/features/summarizer"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
)

const fast = 10 * time.Millisecond

// newHub returns a hub over a config.yaml in a temp dir, holding body (when
// non-empty) with mtime t0, loaded the way the daemon loads at startup.
func newHub(t *testing.T, body string, t0 time.Time) (*SettingsHub, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if body != "" {
		write(t, path, body, t0)
	}
	st := settings.NewStore()
	require.NoError(t, st.Load(path))
	return NewSettingsHub(path, st), path
}

// write replaces path atomically (temp file + rename) with a chosen mtime, so
// a poll never observes a half-done write or a mtime that changes twice, and
// a coarse filesystem clock cannot hide the change.
func write(t *testing.T, path, body string, mtime time.Time) {
	t.Helper()
	tmp := path + ".tmp"
	require.NoError(t, os.WriteFile(tmp, []byte(body), 0o600))
	require.NoError(t, os.Chtimes(tmp, mtime, mtime))
	require.NoError(t, os.Rename(tmp, path))
}

// call records the value of one setting at each notification.
type call struct {
	mu   sync.Mutex
	key  string
	vals []string
}

func (c *call) fn(s *settings.Store) {
	c.mu.Lock()
	c.vals = append(c.vals, s.String(c.key))
	c.mu.Unlock()
}

func (c *call) got() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.vals...)
}

// watch runs hub.Watch and stops it (and waits) when the test ends.
func watch(t *testing.T, h *SettingsHub) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); h.Watch(ctx, fast) }()
	t.Cleanup(func() { cancel(); <-done })
}

func TestSettingsHub_CurrentIsTheInitialStore(t *testing.T) {
	h, _ := newHub(t, "summary_detail: long\n", time.Now())
	require.Equal(t, "long", h.Current().String("summary_detail"))
}

func TestSettingsHub_SubscribersFireOnlyForChangedSubscribedIDs(t *testing.T) {
	t0 := time.Now().Add(-time.Hour)
	h, path := newHub(t, "summary_detail: short\n", t0)
	detail := &call{key: "summary_detail"}
	trust := &call{key: "auto_trust_dirs"}
	both := &call{key: "summary_detail"}
	h.Subscribe([]string{"summary_detail"}, detail.fn)
	h.Subscribe([]string{"auto_trust_dirs"}, trust.fn)
	h.Subscribe([]string{"auto_trust_dirs", "summary_detail"}, both.fn)

	require.NoError(t, h.Reload())
	require.Empty(t, detail.got(), "nothing changed")

	write(t, path, "summary_detail: long\n", t0.Add(time.Minute))
	require.NoError(t, h.Reload())
	require.Equal(t, []string{"long"}, detail.got())
	require.Empty(t, trust.got(), "its id did not change")
	require.Equal(t, []string{"long"}, both.got())
	require.Equal(t, "long", h.Current().String("summary_detail"))

	// An unrelated key changing wakes nobody.
	write(t, path, "summary_detail: long\nusage: false\n", t0.Add(2*time.Minute))
	require.NoError(t, h.Reload())
	require.Len(t, detail.got(), 1)
	require.Len(t, both.got(), 1)

	// A removed file applies the defaults.
	require.NoError(t, os.Remove(path))
	require.NoError(t, h.Reload())
	require.Equal(t, []string{"long", "medium"}, detail.got())
}

func TestSettingsHub_NoIDsMeansAnyChange(t *testing.T) {
	t0 := time.Now().Add(-time.Hour)
	h, path := newHub(t, "", t0)
	var n atomic.Int32
	h.Subscribe(nil, func(*settings.Store) { n.Add(1) })

	require.NoError(t, h.Reload())
	require.Zero(t, n.Load())
	write(t, path, "usage: false\n", t0)
	require.NoError(t, h.Reload())
	require.EqualValues(t, 1, n.Load())
}

func TestSettingsHub_SubscribeRejectsProgrammingErrors(t *testing.T) {
	h, _ := newHub(t, "", time.Now())
	require.Panics(t, func() { h.Subscribe([]string{"no_such_setting"}, func(*settings.Store) {}) })
	require.Panics(t, func() { h.Subscribe([]string{"usage"}, nil) })
}

func TestSettingsHub_ReloadErrorKeepsCurrent(t *testing.T) {
	t0 := time.Now().Add(-time.Hour)
	h, path := newHub(t, "summary_detail: long\n", t0)
	detail := &call{key: "summary_detail"}
	h.Subscribe([]string{"summary_detail"}, detail.fn)

	write(t, path, "summary_detail: [unclosed\n", t0.Add(time.Minute))
	require.Error(t, h.Reload())
	require.Equal(t, "long", h.Current().String("summary_detail"), "a file that fails to load changes nothing")
	require.Empty(t, detail.got())

	write(t, path, "summary_detail: short\n", t0.Add(2*time.Minute))
	require.NoError(t, h.Reload())
	require.Equal(t, []string{"short"}, detail.got())
}

func TestSettingsHub_BrokenFileFailsClosedOnSecuritySettings(t *testing.T) {
	t0 := time.Now().Add(-time.Hour)
	dir := t.TempDir()
	good := "skip_permissions: true\nsummary_detail: long\nauto_trust_dirs: " + dir + "\n"
	h, path := newHub(t, good, t0)
	var cfg server.Config
	h.WireSpawnReads(&cfg, "")
	require.True(t, cfg.SkipPermissions())
	require.Equal(t, []string{dir}, cfg.AutoTrustDirs())

	write(t, path, "skip_permissions: [unclosed\n", t0.Add(time.Minute))
	require.Error(t, h.Reload())
	require.False(t, cfg.SkipPermissions(), "broken file: skip_permissions fails closed")
	require.Empty(t, cfg.AutoTrustDirs(), "broken file: auto_trust_dirs fails closed")
	require.Equal(t, "long", h.Current().String("summary_detail"), "everything else keeps its previous value")

	// Reloading the same broken file again stays closed.
	require.Error(t, h.Reload())
	require.False(t, cfg.SkipPermissions())

	write(t, path, good, t0.Add(2*time.Minute))
	require.NoError(t, h.Reload())
	require.True(t, cfg.SkipPermissions(), "fixed file: restored")
	require.Equal(t, []string{dir}, cfg.AutoTrustDirs())
}

func TestSettingsHub_PollOfBrokenFileFailsClosed(t *testing.T) {
	t0 := time.Now().Add(-time.Hour)
	h, path := newHub(t, "skip_permissions: true\n", t0)
	watch(t, h)
	write(t, path, "skip_permissions: [unclosed\n", t0.Add(time.Minute))
	require.Eventually(t, func() bool { return !SkipPermissions(h.Current()) }, 2*time.Second, 5*time.Millisecond)
	write(t, path, "skip_permissions: true\n", t0.Add(2*time.Minute))
	require.Eventually(t, func() bool { return SkipPermissions(h.Current()) }, 2*time.Second, 5*time.Millisecond)
}

func TestSettingsHub_ReloadWithoutPathIsAnError(t *testing.T) {
	h := NewSettingsHub("", settings.NewStore())
	require.Error(t, h.Reload())
	require.NotNil(t, h.Current())
}

func TestSettingsHub_NilInitialPanics(t *testing.T) {
	require.Panics(t, func() { NewSettingsHub("x", nil) })
}

func TestSettingsHub_WatchAppliesChangesAndCreatedAndRemovedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	h := NewSettingsHub(path, settings.NewStore())
	detail := &call{key: "summary_detail"}
	h.Subscribe([]string{"summary_detail"}, detail.fn)
	watch(t, h)

	time.Sleep(60 * time.Millisecond)
	require.Empty(t, detail.got(), "the state at start counts as applied")

	write(t, path, "summary_detail: long\n", time.Now())
	require.Eventually(t, func() bool { return len(detail.got()) == 1 }, 2*time.Second, 5*time.Millisecond)
	require.Equal(t, "long", h.Current().String("summary_detail"))

	time.Sleep(60 * time.Millisecond)
	require.Len(t, detail.got(), 1, "no change, no apply")

	require.NoError(t, os.Remove(path))
	require.Eventually(t, func() bool { return len(detail.got()) == 2 }, 2*time.Second, 5*time.Millisecond)
	require.Equal(t, "medium", detail.got()[1], "a removed file applies the defaults")
}

func TestSettingsHub_WatchSkipsABrokenFileUntilItChanges(t *testing.T) {
	t0 := time.Now().Add(-time.Hour)
	h, path := newHub(t, "summary_detail: long\n", t0)
	detail := &call{key: "summary_detail"}
	h.Subscribe([]string{"summary_detail"}, detail.fn)
	watch(t, h)

	write(t, path, "summary_detail: [unclosed\n", t0.Add(time.Minute))
	time.Sleep(80 * time.Millisecond)
	require.Empty(t, detail.got())
	require.Equal(t, "long", h.Current().String("summary_detail"))

	write(t, path, "summary_detail: short\n", t0.Add(2*time.Minute))
	require.Eventually(t, func() bool { return len(detail.got()) == 1 }, 2*time.Second, 5*time.Millisecond)
}

func TestSettingsHub_WatchReturnsAtOnceWithNothingToWatch(t *testing.T) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		NewSettingsHub("", settings.NewStore()).Watch(context.Background(), fast)
		NewSettingsHub("x", settings.NewStore()).Watch(context.Background(), 0)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Watch blocked with nothing to do")
	}
}

func TestSettingsHub_LiveReloadOffDoesNotPoll(t *testing.T) {
	t0 := time.Now().Add(-time.Hour)
	h, path := newHub(t, "live_reload: false\nsummary_detail: short\n", t0)
	var stats atomic.Int32
	real := h.stat
	h.stat = func(p string) fileSig { stats.Add(1); return real(p) }
	detail := &call{key: "summary_detail"}
	h.Subscribe([]string{"summary_detail"}, detail.fn)
	base := stats.Load()
	watch(t, h)

	write(t, path, "live_reload: false\nsummary_detail: long\n", t0.Add(time.Minute))
	time.Sleep(100 * time.Millisecond)
	require.Equal(t, base, stats.Load(), "off: the file is never stat'ed")
	require.Empty(t, detail.got())
	require.Equal(t, "short", h.Current().String("summary_detail"))

	// A manual reload still works.
	require.NoError(t, h.Reload())
	require.Equal(t, []string{"long"}, detail.got())
	require.Equal(t, "long", h.Current().String("summary_detail"))
}

func TestSettingsHub_LiveReloadTurnedOnByReloadStartsPolling(t *testing.T) {
	t0 := time.Now().Add(-time.Hour)
	h, path := newHub(t, "live_reload: false\nsummary_detail: short\n", t0)
	detail := &call{key: "summary_detail"}
	h.Subscribe([]string{"summary_detail"}, detail.fn)
	watch(t, h)

	write(t, path, "summary_detail: short\n", t0.Add(time.Minute)) // live_reload back on (default)
	require.NoError(t, h.Reload())

	write(t, path, "summary_detail: long\n", t0.Add(2*time.Minute))
	require.Eventually(t, func() bool { return len(detail.got()) == 1 }, 2*time.Second, 5*time.Millisecond)
}

func TestSettingsHub_LiveReloadTurnedOffByPollStopsPolling(t *testing.T) {
	t0 := time.Now().Add(-time.Hour)
	h, path := newHub(t, "summary_detail: short\n", t0)
	var stats atomic.Int32
	real := h.stat
	h.stat = func(p string) fileSig { stats.Add(1); return real(p) }
	watch(t, h)

	write(t, path, "live_reload: false\n", t0.Add(time.Minute))
	require.Eventually(t, func() bool { return !settings.FeatureOn(h.Current(), "live_reload") }, 2*time.Second, 5*time.Millisecond)
	time.Sleep(50 * time.Millisecond) // let the loop notice
	n := stats.Load()
	time.Sleep(100 * time.Millisecond)
	require.Equal(t, n, stats.Load(), "polling stopped once the flag went off")
}

func TestSummaryDetailApplier_FollowsTheHub(t *testing.T) {
	t0 := time.Now().Add(-time.Hour)
	h, path := newHub(t, "summary_enabled: true\n", t0)
	w := summarizer.New(summarizer.Defaults(), state.NewStore(), func(string, int) []byte { return nil })
	require.Equal(t, summarizer.DetailMedium, w.Detail())
	h.Subscribe([]string{"summary_detail"}, SummaryDetailApplier(w))
	watch(t, h)

	write(t, path, "summary_detail: long\n", t0.Add(time.Minute))
	require.Eventually(t, func() bool { return w.Detail() == summarizer.DetailLong }, 2*time.Second, 5*time.Millisecond)
	write(t, path, "summary_detail: short\n", t0.Add(2*time.Minute))
	require.Eventually(t, func() bool { return w.Detail() == summarizer.DetailShort }, 2*time.Second, 5*time.Millisecond)
	// An invalid value falls back to medium.
	write(t, path, "summary_detail: huge\n", t0.Add(3*time.Minute))
	require.Eventually(t, func() bool { return w.Detail() == summarizer.DetailMedium }, 2*time.Second, 5*time.Millisecond)
}

func TestSummaryScheduleApplier_FollowsTheHub(t *testing.T) {
	t0 := time.Now().Add(-time.Hour)
	h, path := newHub(t, "summary_enabled: true\n", t0)
	// The daemon builds the worker from the hub's current value: on by default.
	cfg := summarizer.Defaults()
	cfg.Schedule = settings.FeatureOn(h.Current(), "summary_schedule")
	w := summarizer.New(cfg, state.NewStore(), func(string, int) []byte { return nil })
	require.True(t, w.Schedule())
	h.Subscribe([]string{"summary_schedule"}, SummaryScheduleApplier(w))
	watch(t, h)

	write(t, path, "summary_schedule: false\n", t0.Add(time.Minute))
	require.Eventually(t, func() bool { return !w.Schedule() }, 2*time.Second, 5*time.Millisecond)
	write(t, path, "summary_schedule: true\n", t0.Add(2*time.Minute))
	require.Eventually(t, w.Schedule, 2*time.Second, 5*time.Millisecond)
	// Removing the key means the default, on.
	write(t, path, "summary_schedule: false\n", t0.Add(3*time.Minute))
	require.Eventually(t, func() bool { return !w.Schedule() }, 2*time.Second, 5*time.Millisecond)
	write(t, path, "summary_detail: long\n", t0.Add(4*time.Minute))
	require.Eventually(t, w.Schedule, 2*time.Second, 5*time.Millisecond)
}

// Spawn-time reads go through hub.Current(), so a Reload is all it takes for
// the next spawn to see a change: no daemon restart.
func TestSpawnTimeReadsSeeChangesAfterReload(t *testing.T) {
	t0 := time.Now().Add(-time.Hour)
	h, path := newHub(t, "", t0)
	dir := t.TempDir()
	cur := h.Current
	require.False(t, SkipPermissions(cur()))
	require.True(t, ResumeEnabled(cur()))
	require.True(t, SpawnSkillsEnabled(cur()))
	require.Empty(t, AutoTrustDirs(cur()))
	defLua, err := paths.LuaInitFile()
	require.NoError(t, err)
	require.Equal(t, defLua, luaConfigPathFrom(cur()), "default: ~/.config/rex/init.lua")

	write(t, path, "skip_permissions: true\nresume: false\nspawn_skills: false\nauto_trust_dirs: "+dir+"\nlua_config_path: "+filepath.Join(dir, "init.lua")+"\n", t0.Add(time.Minute))
	require.NoError(t, h.Reload())
	require.True(t, SkipPermissions(cur()))
	require.False(t, ResumeEnabled(cur()))
	require.False(t, SpawnSkillsEnabled(cur()))
	require.Equal(t, []string{dir}, AutoTrustDirs(cur()))
	require.Equal(t, filepath.Join(dir, "init.lua"), luaConfigPathFrom(cur()))
}

func TestWireSpawnReads_FollowTheHub(t *testing.T) {
	t0 := time.Now().Add(-time.Hour)
	h, path := newHub(t, "", t0)
	var cfg server.Config
	h.WireSpawnReads(&cfg, "/some/other.sock")
	dir := t.TempDir()
	require.False(t, cfg.SkipPermissions())
	require.True(t, cfg.ResumeEnabled())
	require.True(t, cfg.SkillsEnabled())
	require.False(t, cfg.SnapshotsEnabled())
	require.Empty(t, cfg.AutoTrustDirs())
	require.Empty(t, cfg.UsageTap("s1"), "usage_statusline_tap auto, no user status line")

	write(t, path, "skip_permissions: true\nresume: false\nspawn_skills: false\nturn_snapshots: true\nusage_statusline_tap: \"on\"\nauto_trust_dirs: "+dir+"\n", t0.Add(time.Minute))
	require.NoError(t, h.Reload())
	require.True(t, cfg.SkipPermissions())
	require.False(t, cfg.ResumeEnabled())
	require.False(t, cfg.SkillsEnabled())
	require.True(t, cfg.SnapshotsEnabled())
	require.Equal(t, []string{dir}, cfg.AutoTrustDirs())
	require.Contains(t, cfg.UsageTap("s1"), "statusline-tap --session s1")
}

func TestReloadAll_ReloadsRegistryThenSettings(t *testing.T) {
	t0 := time.Now().Add(-time.Hour)
	h, cfg := newHub(t, "summary_detail: short\n", t0)
	detail := &call{key: "summary_detail"}
	h.Subscribe([]string{"summary_detail"}, detail.fn)
	srv, tools := newReloadServer(t, "")

	write(t, cfg, "summary_detail: long\n", t0.Add(time.Minute))
	require.NoError(t, os.WriteFile(tools, []byte("tools: []\n"), 0o600))
	before := srv.Registry()
	require.NoError(t, reloadAll(t, srv, tools, h))
	require.NotSame(t, before, srv.Registry(), "the registry was swapped")
	require.Equal(t, []string{"long"}, detail.got())
}

func TestReloadAll_RegistryErrorStillReloadsSettings(t *testing.T) {
	t0 := time.Now().Add(-time.Hour)
	h, cfg := newHub(t, "summary_detail: short\n", t0)
	detail := &call{key: "summary_detail"}
	h.Subscribe([]string{"summary_detail"}, detail.fn)
	srv, tools := newReloadServer(t, "")

	write(t, cfg, "summary_detail: long\n", t0.Add(time.Minute))
	require.NoError(t, os.WriteFile(tools, []byte("tools: [unclosed\n"), 0o600))
	require.Error(t, reloadAll(t, srv, tools, h))
	require.Equal(t, []string{"long"}, detail.got(), "a bad tools.yaml must not hold back settings")
}

func TestReloadAll_SettingsErrorIsReported(t *testing.T) {
	t0 := time.Now().Add(-time.Hour)
	h, cfg := newHub(t, "summary_detail: short\n", t0)
	srv, tools := newReloadServer(t, "")
	write(t, cfg, "summary_detail: [unclosed\n", t0.Add(time.Minute))
	require.Error(t, reloadAll(t, srv, tools, h))
}

func newReloadServer(t *testing.T, toolsBody string) (*server.Server, string) {
	t.Helper()
	dir := t.TempDir()
	tools := filepath.Join(dir, "tools.yaml")
	if toolsBody != "" {
		require.NoError(t, os.WriteFile(tools, []byte(toolsBody), 0o600))
	}
	reg, err := registry.Load("")
	require.NoError(t, err)
	srv, err := server.New(server.Config{Socket: filepath.Join(dir, "rex.sock"), StateDir: dir, Registry: reg, Store: state.NewStore()})
	require.NoError(t, err)
	return srv, tools
}

// reloadAll runs ReloadAll with a PATH that holds no agent, so the model
// refresh a reload starts can never run a real agent binary.
func reloadAll(t *testing.T, srv *server.Server, tools string, h *SettingsHub) error {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
	cat := NewCatalog(srv, tools)
	defer cat.Wait()
	rep := ReloadAll(context.Background(), cat, h, nil)
	if len(rep.Errors) > 0 {
		return errors.New(strings.Join(rep.Errors, "; "))
	}
	return nil
}

// R20: the reload report lists the changed ids, the ones that need a
// daemon restart, and the layers read.
func TestReloadAll_ReportsChangedAndRestart(t *testing.T) {
	t0 := time.Now().Add(-time.Hour)
	h, cfg := newHub(t, "summary_detail: short\n", t0)
	srv, tools := newReloadServer(t, "")
	write(t, cfg, "summary_detail: long\nsummary_model: other-model\n", t0.Add(time.Minute))
	t.Setenv("PATH", t.TempDir())
	cat := NewCatalog(srv, tools)
	defer cat.Wait()
	rep := ReloadAll(context.Background(), cat, h, nil)
	require.Empty(t, rep.Errors)
	require.ElementsMatch(t, []string{"summary_detail", "summary_model"}, rep.Changed)
	require.Equal(t, []string{"summary_model"}, rep.Restart)
	require.Contains(t, rep.Reloaded, "tools.yaml")
	require.Contains(t, rep.Reloaded, "config.yaml")

	rep = ReloadAll(context.Background(), cat, h, nil)
	require.Empty(t, rep.Changed, "a second reload with no edit changes nothing")
}

func TestRetireTTLApplier_FollowsTheHub(t *testing.T) {
	t0 := time.Now().Add(-time.Hour)
	h, path := newHub(t, "retire_after_hours: 2\n", t0)
	srv, _ := newReloadServer(t, "")
	h.Subscribe([]string{"retire_after_hours"}, RetireTTLApplier(srv))
	write(t, path, "retire_after_hours: 5\n", t0.Add(time.Minute))
	require.NoError(t, h.Reload())
	require.Equal(t, 5*time.Hour, srv.RetireDefaultTTL())
}
