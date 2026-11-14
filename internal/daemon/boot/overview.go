package boot

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/daemon/server"
	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/features/overview"
	"github.com/tristanbietsch/rex/internal/features/summarizer"
)

// Overview wires the overview runner to the store, the server and the
// summarizer. It is inert while the overview_enabled setting is off: no
// goroutine runs until the first time it is switched on, no model is called
// and nothing is published. The headline is never logged or persisted.
type Overview struct {
	ctx    context.Context
	runner *overview.Runner
	unsub  func()
	once   sync.Once
}

// NewOverview builds the overview for store, srv and w, timed by cfg. It starts nothing: a
// store subscription that drops every event while disabled is all it holds.
// Call SetEnabled with the initial setting, and Close when done.
func NewOverview(ctx context.Context, cfg overview.Config, store *state.Store, srv *server.Server, w *summarizer.Worker) *Overview {
	o := &Overview{ctx: ctx}
	o.runner = overview.NewRunner(cfg,
		store.Snapshot,
		func(ctx context.Context, digest string) (string, error) {
			h, err := w.Summarize(ctx, summarizer.KindFleet, summarizer.Input{Text: digest})
			if errors.Is(err, summarizer.ErrUnavailable) {
				return "", overview.ErrUnavailable
			}
			return h, err
		},
		srv.SetOverview,
		func() bool { return srv.BoardClients() > 0 })
	o.unsub = store.Subscribe(func(e state.Event) { o.runner.Notify(OverviewRelevance(e)) })
	return o
}

// OverviewRelevance rates a store event for the overview runner: a session
// added or removed, or a patch that changes its bucket or name, is
// structural; a new description is content; anything else is ignored.
func OverviewRelevance(e state.Event) overview.Relevance {
	switch e.Kind {
	case state.EventAdded, state.EventRemoved:
		return overview.StructuralChange
	case state.EventUpdated:
		if e.NewState != nil {
			return overview.StructuralChange
		}
		return overview.PatchRelevance(e.Patch)
	case state.EventReopened:
		// The state patch that follows carries the change.
	}
	return overview.Ignored
}

// SetEnabled switches the overview on or off. The first switch on starts the
// runner's goroutine; it then lives until the daemon stops, parked while off.
func (o *Overview) SetEnabled(on bool) {
	if on {
		o.once.Do(func() {
			go func() {
				if err := o.runner.Run(o.ctx); err != nil && !errors.Is(err, context.Canceled) {
					slog.Warn("overview: runner exited", "err", err)
				}
			}()
		})
	}
	slog.Info("daemon: overview", "enabled", on)
	o.runner.SetEnabled(on)
}

// Notify tells the runner the board changed. A nil Overview (no summarizer)
// ignores it.
func (o *Overview) Notify(rel overview.Relevance) {
	if o != nil {
		o.runner.Notify(rel)
	}
}

// Applier is the SettingsHub subscriber that follows overview_enabled.
func (o *Overview) Applier() func(*settings.Store) {
	return func(s *settings.Store) { o.SetEnabled(OverviewEnabled(s)) }
}

// Close stops listening to the store.
func (o *Overview) Close() { o.unsub() }

// OverviewEnabled reads the overview_enabled setting from st (false when st
// is nil).
func OverviewEnabled(st *settings.Store) bool {
	if st == nil {
		return false
	}
	on, _ := st.Get("overview_enabled").(bool)
	return on
}
