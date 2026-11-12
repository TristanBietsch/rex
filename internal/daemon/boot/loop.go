package boot

import (
	"context"
	"log/slog"
	"path/filepath"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/daemon/server"
	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/daemon/termtext"
	"github.com/tristanbietsch/rex/internal/features/loop"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// loopOutputBytes bounds the output a marker is matched against.
const loopOutputBytes = 16 << 10

// LoopsEnabled reads the loops feature switch from config.yaml. On a read
// error it uses the default (off).
func LoopsEnabled() bool {
	st, _, err := settings.LoadDefault()
	if err != nil {
		slog.Warn("daemon: settings load failed for loops; using the default", "err", err)
	}
	return settings.FeatureOn(st, "loops")
}

// Loops wires the loop runner to the daemon: it is the runner's Host over
// the server and session store, and the server's LoopService. While the
// loops feature is off the runner is never enabled: no goroutine, no store
// subscription, no loop file or state read.
type Loops struct {
	Runner   *loop.Runner
	flag     func() bool
	store    *state.Store
	stateDir string
	srv      *server.Server
}

// NewLoops builds the (disabled) loop runner. loopsDir is where loop files
// live; stateDir is the daemon's state dir. flag reports the loops setting
// and is read at every intent. sessionCtx is the daemon's serve context:
// loop sessions are spawned and resumed under it, like every session, so the
// runner stopping never kills an agent.
func NewLoops(sessionCtx context.Context, stateDir, loopsDir string, store *state.Store, flag func() bool) *Loops {
	l := &Loops{flag: flag, store: store, stateDir: stateDir}
	l.Runner = loop.NewRunner(loop.Config{
		Host:       l,
		StateDir:   filepath.Join(stateDir, paths.LoopStateDirName),
		SessionCtx: sessionCtx,
		LoadDef:    func(name string) (loop.Def, error) { return loop.LoadFile(loopsDir, name) },
	})
	return l
}

// SetServer gives the host its server. Call it before the first intent.
func (l *Loops) SetServer(srv *server.Server) { l.srv = srv }

// SetEnabled switches the runner on or off, following the setting.
func (l *Loops) SetEnabled(on bool) {
	if !on {
		l.Runner.Disable()
		return
	}
	if err := l.Runner.Enable(); err != nil {
		slog.Warn("daemon: loops could not start", "err", err)
	}
}

// Applier is the WatchSettings applier that follows the loops setting.
func (l *Loops) Applier() func(*settings.Store) {
	return func(st *settings.Store) { l.SetEnabled(settings.FeatureOn(st, "loops")) }
}

// Close pauses live loops and stops the runner.
func (l *Loops) Close() { l.Runner.Close() }

// ready refuses while the feature is off and otherwise makes sure the
// runner runs (the settings watcher may not have noticed a change yet).
func (l *Loops) ready() error {
	if !l.flag() {
		l.Runner.Disable()
		return loop.ErrOff
	}
	return l.Runner.Enable()
}

func (l *Loops) call(f func() (protocol.LoopSummary, error)) (protocol.LoopSummary, error) {
	if err := l.ready(); err != nil {
		return protocol.LoopSummary{}, err
	}
	return f()
}

// --- server.LoopService ---

func (l *Loops) Start(p protocol.StartLoop) (protocol.LoopSummary, error) {
	return l.call(func() (protocol.LoopSummary, error) { return l.Runner.Start(p) })
}

func (l *Loops) Pause(id string) (protocol.LoopSummary, error) {
	return l.call(func() (protocol.LoopSummary, error) { return l.Runner.Pause(id) })
}

func (l *Loops) Resume(id string) (protocol.LoopSummary, error) {
	return l.call(func() (protocol.LoopSummary, error) { return l.Runner.Resume(id) })
}

func (l *Loops) Stop(id string) (protocol.LoopSummary, error) {
	return l.call(func() (protocol.LoopSummary, error) { return l.Runner.Stop(id) })
}

func (l *Loops) Snapshot() []protocol.LoopSummary { return l.Runner.Summaries() }

func (l *Loops) UserInput(sessionID string) { l.Runner.UserInput(sessionID) }

// --- loop.Host ---

func (l *Loops) Spawn(ctx context.Context, req loop.SpawnRequest) (string, error) {
	return l.srv.SpawnLoopSession(ctx, req)
}

func (l *Loops) Send(sessionID, text string, guard func() bool) (bool, error) {
	return l.srv.SendText(sessionID, text, guard)
}

func (l *Loops) Session(id string) (protocol.SessionSummary, bool) {
	sess, ok := l.store.Get(id)
	if !ok {
		return protocol.SessionSummary{}, false
	}
	return sess.Summary(), true
}

// Output is the session's screen, else the tail of its cleaned transcript:
// the same view the summarizer reads.
func (l *Loops) Output(id string) string {
	if scr := l.store.Screen(id); scr != "" {
		return scr
	}
	b, _ := state.TranscriptTail(l.stateDir, id, 4*loopOutputBytes)
	return termtext.Tail(b, loopOutputBytes)
}

// ReportsReason says whether the tool tells a finished turn from a prompt
// that needs the user, which is what a loop needs to never answer a prompt.
// Today that is Claude Code's hooks only. A tool opts in here once its
// adapter reports a wait reason (the OSC 7501 tools, with R02).
func (l *Loops) ReportsReason(toolID string) bool {
	tool, ok := l.srv.Registry().Find(toolID)
	return ok && toolReportsReason(tool)
}

func toolReportsReason(t registry.Tool) bool {
	switch {
	case t.Detect.Kind == "hooks" && t.Detect.Format == "claude":
		return true
	case t.Detect.Status == registry.StatusOSC7501:
		return false // the adapter does not report a wait reason yet
	}
	return false
}

func (l *Loops) EnsureLive(ctx context.Context, id string) error { return l.srv.EnsureLive(ctx, id) }

func (l *Loops) SetSessionLoop(id, loopID string, iter int) { l.srv.SetSessionLoop(id, loopID, iter) }

func (l *Loops) Subscribe(fn func(loop.Event)) func() {
	return l.store.Subscribe(func(e state.Event) {
		switch e.Kind {
		case state.EventAdded:
			// A new session is nobody's loop yet; its state events follow.
		case state.EventRemoved:
			fn(loop.Event{SessionID: e.SessionID, Removed: true})
		case state.EventUpdated:
			ev := loop.Event{SessionID: e.SessionID}
			if e.NewState != nil {
				ev.State = *e.NewState
			}
			if v, ok := e.Patch["wait_reason"]; ok {
				r, _ := v.(string)
				ev.WaitReason = &r
			}
			if ev.State != "" || ev.WaitReason != nil {
				fn(ev)
			}
		}
	})
}

func (l *Loops) Publish(s protocol.LoopSummary) { l.srv.PublishLoop(s) }
