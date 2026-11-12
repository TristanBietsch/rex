package boot

import (
	"context"
	"path/filepath"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/features/usage"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// Usage wires the usage hub to the store. It is inert while the usage
// setting is off: it reads no file and starts nothing until the first time
// the setting is on.
type Usage struct {
	Hub   *usage.Hub
	unsub func()
}

// NewUsage builds the usage hub for store, keeping its ledger in stateDir.
// publish receives the live usage state whenever a quota changes or the hub
// is switched on or off (nil: nobody listens). Call SetEnabled with the
// initial setting, and Close when done.
func NewUsage(ctx context.Context, stateDir string, store *state.Store, publish func(protocol.Usage)) *Usage {
	hub := usage.NewHub(ctx, usage.HubConfig{
		Ledger:   usage.OpenLedger(filepath.Join(stateDir, paths.UsageLedgerFileName)),
		Sessions: store.Snapshot,
		OnUsage:  publish,
	})
	u := &Usage{Hub: hub}
	// The callback only queues work (Hub.Observe never touches a file), as
	// the store requires of a subscriber.
	u.unsub = store.Subscribe(func(e state.Event) {
		if !hub.Enabled() {
			return
		}
		switch e.Kind {
		case state.EventAdded, state.EventUpdated:
			if sess, ok := store.Get(e.SessionID); ok {
				hub.Observe(sess.Summary())
			}
		case state.EventRemoved:
			hub.Removed(e.SessionID)
		}
	})
	return u
}

// SetEnabled switches usage tracking on or off.
func (u *Usage) SetEnabled(on bool) { u.Hub.SetEnabled(on) }

// Applier is the SettingsHub subscriber that follows the usage setting.
func (u *Usage) Applier() func(*settings.Store) {
	return func(s *settings.Store) { u.SetEnabled(settings.FeatureOn(s, "usage")) }
}

// Close stops listening to the store and flushes the hub.
func (u *Usage) Close() {
	u.unsub()
	u.Hub.Close()
}
