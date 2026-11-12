package boot

import (
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
)

// fakeLayer is a settings layer whose report and stamp a test changes.
type fakeLayer struct {
	mu     sync.Mutex
	rep    settings.LayerReport
	stamp  string
	stamps atomic.Int32
	evals  atomic.Int32
}

func (f *fakeLayer) set(rep settings.LayerReport, stamp string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rep, f.stamp = rep, stamp
}

func installFakeLayer(t *testing.T) *fakeLayer {
	t.Helper()
	f := &fakeLayer{}
	settings.SetLayerStamped(func() settings.LayerReport {
		f.evals.Add(1)
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.rep
	}, func() string {
		f.stamps.Add(1)
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.stamp
	})
	t.Cleanup(func() { settings.SetLayer(nil) })
	return f
}

func luaRep(vals map[string]any) settings.LayerReport {
	at := map[string]string{}
	for k := range vals {
		at[k] = "config.lua:1"
	}
	return settings.LayerReport{Exists: true, Values: vals, SetAt: at}
}

func TestSettingsHub_ReloadAppliesTheLuaLayer(t *testing.T) {
	f := installFakeLayer(t)
	f.set(luaRep(map[string]any{"summary_detail": "long"}), "s1")
	h, _ := newHub(t, "summary_detail: short\n", time.Now())
	require.NoError(t, h.Reload())
	require.Equal(t, "long", h.Current().String("summary_detail"))
	require.Equal(t, settings.SourceLua, h.Current().Source("summary_detail"))
}

func TestSettingsHub_PollSeesConfigLuaEdits(t *testing.T) {
	f := installFakeLayer(t)
	f.set(luaRep(map[string]any{"summary_detail": "medium"}), "s1")
	t0 := time.Now().Add(-time.Hour)
	h, _ := newHub(t, "summary_detail: short\n", t0)
	require.NoError(t, h.Reload())
	detail := &call{key: "summary_detail"}
	h.Subscribe([]string{"summary_detail"}, detail.fn)
	watch(t, h)

	// config.yaml is unchanged; only the Lua stamp moves.
	f.set(luaRep(map[string]any{"summary_detail": "long"}), "s2")
	require.Eventually(t, func() bool { return h.Current().String("summary_detail") == "long" }, 2*time.Second, 5*time.Millisecond)
	require.Equal(t, []string{"long"}, detail.got())
}

// TestLuaErrorKeepsLastGoodButFailsTrustClosed: Lua sets auto_trust_dirs=""
// over YAML "~/dev"; once the file breaks the hub's Current is "", not the
// YAML value, while the other last good values stay.
func TestLuaErrorKeepsLastGoodButFailsTrustClosed(t *testing.T) {
	f := installFakeLayer(t)
	good := luaRep(map[string]any{"auto_trust_dirs": "", "summary_detail": "long"})
	f.set(good, "s1")
	h, _ := newHub(t, "auto_trust_dirs: ~/dev\nsummary_detail: short\n", time.Now())
	require.NoError(t, h.Reload())
	require.Equal(t, "", h.Current().String("auto_trust_dirs"))

	broken := good
	broken.Err, broken.LastGood = errors.New("config.lua:3: boom"), true
	f.set(broken, "s2")
	require.NoError(t, h.Reload(), "a config.lua error is not a reload error")
	require.Equal(t, "", h.Current().String("auto_trust_dirs"))
	require.Equal(t, "long", h.Current().String("summary_detail"))
}

func TestSettingsHub_LuaConfigOffNeverStampsTheLayer(t *testing.T) {
	f := installFakeLayer(t)
	f.set(luaRep(map[string]any{"summary_detail": "long"}), "s1")
	t0 := time.Now().Add(-time.Hour)
	h, path := newHub(t, "lua_config: false\nsummary_detail: short\n", t0)
	require.NoError(t, h.Reload())
	watch(t, h)
	write(t, path, "lua_config: false\nsummary_detail: medium\n", t0.Add(time.Minute))
	require.Eventually(t, func() bool { return h.Current().String("summary_detail") == "medium" }, 2*time.Second, 5*time.Millisecond)
	time.Sleep(50 * time.Millisecond)
	require.Zero(t, f.stamps.Load(), "off: config.lua is never stat'ed")
	require.Zero(t, f.evals.Load(), "off: config.lua is never evaluated")
	require.False(t, h.Current().Locked("summary_detail"))
}

// The daemon and the TUI load the same files the same way.
func TestSettingsHub_SnapshotMatchesLoadLayered(t *testing.T) {
	f := installFakeLayer(t)
	f.set(luaRep(map[string]any{"summary_detail": "long", "theme": "noir"}), "s1")
	h, path := newHub(t, "spinner: moon\n", time.Now())
	require.NoError(t, h.Reload())
	other, _, err := settings.LoadLayered(path)
	require.NoError(t, err)
	require.Equal(t, other.Snapshot(), h.Current().Snapshot())
}

func TestSettingsHub_LuaErrorLogIsFlattened(t *testing.T) {
	var buf safeBuf
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })
	f := installFakeLayer(t)
	f.set(settings.LayerReport{Exists: true, Err: errors.New("\x1b[2Jboom\x1b]0;x\x07"), Warnings: []string{"w\x1b[31m"}}, "s1")
	h, _ := newHub(t, "", time.Now())
	require.NoError(t, h.Reload())
	out := buf.String()
	require.Contains(t, out, "config.lua failed")
	require.Contains(t, out, "config.lua warnings")
	require.NotContains(t, out, "\x1b")
	require.NotContains(t, out, "\x07")
}

type safeBuf struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *safeBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *safeBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}
