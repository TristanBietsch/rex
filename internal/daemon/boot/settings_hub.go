package boot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/daemon/server"
	"github.com/tristanbietsch/rex/internal/features/summarizer"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// SettingsHub is the daemon's one view of config.yaml (with config.lua on
// top while lua_config is on, settings.LoadLayered). Spawn-time reads call
// Current instead of re-reading the file; features Subscribe to the settings
// they follow instead of polling; Reload (SIGHUP, the Reload intent, a hand
// edit noticed by Watch) swaps in a fresh Store and wakes the subscribers of
// whatever changed. There is one poller for the whole daemon: Watch.
//
// The Store Current returns is shared and read-only: nobody may Set, Reset
// or Save it. A reload builds a new one.
type SettingsHub struct {
	path string
	cur  atomic.Pointer[settings.Store]
	stat func(string) fileSig // test seam

	// mu serializes reloads and guards last. Subscribers run under it, so
	// they must not call Reload or Subscribe.
	mu   sync.Mutex
	last hubSig // state of the files as of the last load

	subMu sync.Mutex
	subs  []hubSub

	// wake tells Watch the live_reload flag may have changed.
	wake chan struct{}
}

type hubSub struct {
	ids []string // never empty
	fn  func(*settings.Store)
}

// NewSettingsHub returns a hub over the settings file at path, holding
// initial (the store the daemon loaded at startup). The file's state now
// counts as applied: Watch reacts only to later changes. An empty path
// (home directory unknown) gives a hub that holds initial and cannot reload.
func NewSettingsHub(path string, initial *settings.Store) *SettingsHub {
	if initial == nil {
		panic("boot: NewSettingsHub: nil initial store")
	}
	h := &SettingsHub{path: path, stat: statSig, wake: make(chan struct{}, 1)}
	h.cur.Store(initial)
	h.last = h.sig()
	return h
}

// Current returns the settings in force. It is safe for concurrent use and
// never nil.
func (h *SettingsHub) Current() *settings.Store { return h.cur.Load() }

// Subscribe calls fn with the new store after a reload in which any of ids
// changed value. With no ids it is any setting. fn runs on the goroutine that
// reloaded, one subscriber after another in subscription order, and must not
// block. An id that is not a setting, or a nil fn, is a programming error and
// panics.
func (h *SettingsHub) Subscribe(ids []string, fn func(*settings.Store)) {
	if fn == nil {
		panic("boot: SettingsHub.Subscribe: nil fn")
	}
	if len(ids) == 0 {
		for _, s := range settings.Registry {
			ids = append(ids, s.ID)
		}
	}
	for _, id := range ids {
		if _, ok := settings.Find(id); !ok {
			panic(fmt.Sprintf("boot: SettingsHub.Subscribe: unknown setting %q", id))
		}
	}
	h.subMu.Lock()
	h.subs = append(h.subs, hubSub{ids: append([]string(nil), ids...), fn: fn})
	h.subMu.Unlock()
}

// Reload re-reads the settings file now, whatever live_reload says, makes the
// result current and calls the subscribers of every setting that changed. A
// missing file loads the defaults. A file that cannot be read or parsed is an
// error: the previous values stay in force, except skip_permissions and
// auto_trust_dirs, which fail closed (off, empty) until the file parses.
func (h *SettingsHub) Reload() error {
	if h.path == "" {
		return errors.New("settings reload: no config file path (home directory unknown)")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.reloadLocked()
}

func (h *SettingsHub) reloadLocked() error {
	// Stat before reading: an edit landing during the read is seen by the
	// next poll rather than lost.
	sig := h.sig()
	st, rep, err := settings.LoadLayered(h.path)
	if err != nil {
		h.last = sig // report a broken file once, not at every poll
		// Fail closed on the settings that grant trust: until the file
		// parses again they read as their defaults. Everything else keeps
		// its previous value. The log names the file, never its contents.
		slog.Warn("daemon: settings reload failed; skip_permissions and auto_trust_dirs are off until config.yaml parses (other settings keep their values)", "path", h.path)
		closed := h.cur.Load().WithDefaults(settings.TrustSettingIDs...)
		old := h.cur.Swap(closed)
		h.notify(old, closed)
		return fmt.Errorf("settings reload: %w", err)
	}
	if rep.Err != nil {
		// The text is flattened by the evaluator; trust settings already
		// read their defaults (settings.LoadLayered).
		slog.Warn("daemon: config.lua failed; skip_permissions and auto_trust_dirs are off until it runs again", "err", protocol.FlattenDisplayText(rep.Err.Error()), "last_good", rep.LastGood)
	}
	if n := len(rep.Warnings); n > 0 {
		slog.Warn("daemon: config.lua warnings", "count", n, "first", protocol.FlattenDisplayText(rep.Warnings[0]))
	}
	h.last = sig
	old := h.cur.Swap(st)
	h.notify(old, st)
	select { // live_reload may have flipped
	case h.wake <- struct{}{}:
	default:
	}
	return nil
}

func (h *SettingsHub) notify(old, cur *settings.Store) {
	h.subMu.Lock()
	subs := append([]hubSub(nil), h.subs...)
	h.subMu.Unlock()
	for _, s := range subs {
		if changed(old, cur, s.ids) {
			s.fn(cur)
		}
	}
}

func changed(old, cur *settings.Store, ids []string) bool {
	for _, id := range ids {
		if !reflect.DeepEqual(old.Get(id), cur.Get(id)) {
			return true
		}
	}
	return false
}

// Watch polls the settings file every `every` and, when it changes
// (modified, replaced, created or removed), reloads as Reload does; a
// failure is logged and skipped until the file changes again. It blocks
// until ctx is canceled and returns at once with no path or no interval.
//
// While the live_reload feature is off Watch does not poll: it parks, with no
// ticker and no stat, until a manual Reload turns the flag back on. The flag
// is read from the current settings, so editing it takes effect at the next
// poll (to turn polling off) or the next Reload (to turn it back on).
func (h *SettingsHub) Watch(ctx context.Context, every time.Duration) {
	if h.path == "" || every <= 0 {
		return
	}
	var tick *time.Ticker
	var tc <-chan time.Time
	defer func() {
		if tick != nil {
			tick.Stop()
		}
	}()
	for {
		on := settings.FeatureOn(h.Current(), "live_reload")
		switch {
		case on && tick == nil:
			tick = time.NewTicker(every)
			tc = tick.C
		case !on && tick != nil:
			tick.Stop()
			tick, tc = nil, nil
		}
		select {
		case <-ctx.Done():
			return
		case <-h.wake:
		case <-tc:
			h.poll()
		}
	}
}

// poll reloads when the file's state differs from the last load.
func (h *SettingsHub) poll() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.sig() == h.last {
		return
	}
	slog.Info("daemon: settings changed, applying")
	_ = h.reloadLocked() // a failure is logged there
}

// hubSig identifies one state of the settings files: config.yaml, plus
// config.lua and lua/ (the layer's stamp) while lua_config is on.
type hubSig struct {
	yaml fileSig
	lua  string
}

func (h *SettingsHub) sig() hubSig {
	return hubSig{yaml: h.stat(h.path), lua: settings.LayerStamp(settings.LuaConfigOn(h.Current()))}
}

// fileSig identifies one state of the settings file. exists is false when it
// is missing (or unreadable), which is its own state.
type fileSig struct {
	exists  bool
	modTime time.Time
	size    int64
}

func statSig(path string) fileSig {
	fi, err := os.Stat(path)
	if err != nil {
		return fileSig{}
	}
	return fileSig{exists: true, modTime: fi.ModTime(), size: fi.Size()}
}

// SummaryDetailApplier returns a SettingsHub subscriber that keeps w's
// description detail in step with the summary_detail setting.
func SummaryDetailApplier(w *summarizer.Worker) func(*settings.Store) {
	return func(s *settings.Store) {
		d := summarizer.ParseDetail(s.String("summary_detail"))
		if d != w.Detail() {
			slog.Info("daemon: summary detail changed", "detail", d.String())
		}
		w.SetDetail(d)
	}
}

// SummaryScheduleApplier returns a SettingsHub subscriber that keeps w's
// scheduled refresh in step with the summary_schedule feature.
func SummaryScheduleApplier(w *summarizer.Worker) func(*settings.Store) {
	return func(s *settings.Store) {
		w.SetSchedule(settings.FeatureOn(s, "summary_schedule"))
	}
}

// WireSpawnReads points cfg's spawn-time settings reads (AutoTrustDirs,
// SkipPermissions, ResumeEnabled, SkillsEnabled, SnapshotsEnabled, UsageTap)
// at the hub's current settings, so each spawn sees the latest reload. socket
// is the daemon's own socket, as UsageTap takes it.
func (h *SettingsHub) WireSpawnReads(cfg *server.Config, socket string) {
	cfg.AutoTrustDirs = func() []string { return AutoTrustDirs(h.Current()) }
	cfg.SkipPermissions = func() bool { return SkipPermissions(h.Current()) }
	cfg.ResumeEnabled = func() bool { return ResumeEnabled(h.Current()) }
	cfg.SkillsEnabled = func() bool { return SpawnSkillsEnabled(h.Current()) }
	cfg.SnapshotsEnabled = func() bool { return TurnSnapshotsEnabled(h.Current()) }
	cfg.UsageTap = UsageTap(socket, h.Current)
}
