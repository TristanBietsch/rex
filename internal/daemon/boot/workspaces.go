package boot

import (
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
	"github.com/tristanbietsch/rex/internal/workspace"
)

// workspacesQueueSize bounds the hand-off from the store callback to the
// index writer.
const workspacesQueueSize = 256

// Workspaces tags new sessions with the workspace around their CWD and keeps
// each workspace's .rex/sessions.jsonl index: a line at spawn and one at the
// session's first terminal state. It implements server.WorkspaceHook.
//
// It is inert while the workspaces setting is off: no discovery, no registry
// or index write. The daemon is the only source of a session's workspace id;
// a client never sends one.
type Workspaces struct {
	home string
	reg  *workspace.Registry
	on   atomic.Bool

	// find is workspace.Find; tests replace it to count lookups.
	find func(cwd, home string) (*workspace.Workspace, error)

	warned   sync.Map // key -> struct{}: one notice per key per daemon run
	warnings atomic.Int32

	queue chan wsJob
	store *state.Store
	unsub func()
	done  chan struct{}
	once  sync.Once

	mu    sync.Mutex
	ended map[string]time.Time // session id -> ended_at already indexed
}

// WorkspacesConfig is NewWorkspaces' input.
type WorkspacesConfig struct {
	// Home bounds discovery ("" turns it off).
	Home string
	// Registry records the workspaces the daemon sees; nil skips that.
	Registry *workspace.Registry
}

type wsJob struct {
	sum protocol.SessionSummary
}

// WorkspacesSettingIDs are the settings Configure reads.
var WorkspacesSettingIDs = []string{"workspaces"}

// NewWorkspaces builds the tagger for store and starts listening for
// terminal states. It starts off: call Configure with the settings. Close
// stops it after the queued lines are written.
func NewWorkspaces(cfg WorkspacesConfig, store *state.Store) *Workspaces {
	w := &Workspaces{
		home:  cfg.Home,
		reg:   cfg.Registry,
		find:  workspace.Find,
		queue: make(chan wsJob, workspacesQueueSize),
		store: store,
		done:  make(chan struct{}),
		ended: map[string]time.Time{},
	}
	// The callback only queues (the store forbids I/O in it).
	w.unsub = store.Subscribe(func(e state.Event) {
		if !w.on.Load() || e.Kind != state.EventUpdated || e.NewState == nil || !state.IsTerminal(*e.NewState) {
			return
		}
		sess, ok := store.Get(e.SessionID)
		if !ok {
			return
		}
		sum := sess.Summary()
		if sum.WorkspaceID == "" {
			return
		}
		select {
		case w.queue <- wsJob{sum: sum}:
		default:
			slog.Warn("workspaces: queue full; the end of a session is not indexed", "session", e.SessionID)
		}
	})
	go w.run()
	return w
}

func (w *Workspaces) run() {
	defer close(w.done)
	for job := range w.queue {
		w.writeEnd(job.sum)
	}
}

// Configure applies the workspaces setting.
func (w *Workspaces) Configure(s *settings.Store) { w.on.Store(settings.FeatureOn(s, "workspaces")) }

// Applier is the SettingsHub subscriber that follows the setting.
func (w *Workspaces) Applier() func(*settings.Store) { return w.Configure }

// Enabled reports whether the workspaces setting is on.
func (w *Workspaces) Enabled() bool { return w.on.Load() }

// SetEnabled switches the feature (tests; the daemon uses Configure).
func (w *Workspaces) SetEnabled(on bool) { w.on.Store(on) }

// Tag sets sess.WorkspaceID from the nearest .rex/ above sess.CWD and
// registers the workspace. An unusable .rex/ (symlink, unsafe owner or mode,
// bad file) leaves the session untagged with one notice.
func (w *Workspaces) Tag(sess *state.Session) {
	if !w.on.Load() || w.home == "" {
		return
	}
	ws, err := w.find(sess.CWD, w.home)
	if err != nil {
		w.warnOnce("find:"+sess.CWD, "workspaces: ignoring this folder's .rex/", err)
		return
	}
	if ws == nil {
		return
	}
	sess.WorkspaceID = ws.ID
	if w.reg != nil {
		if err := w.reg.Seen(ws); err != nil {
			w.warnOnce("registry", "workspaces: cannot update the registry", err)
		}
	}
}

// Started appends the session's spawn line to its workspace index.
func (w *Workspaces) Started(sess *state.Session) {
	if !w.on.Load() {
		return
	}
	w.append(sess.Summary())
}

func (w *Workspaces) writeEnd(sum protocol.SessionSummary) {
	if !w.on.Load() {
		return
	}
	w.mu.Lock()
	if prev, ok := w.ended[sum.ID]; ok && prev.Equal(sum.EndedAt) {
		w.mu.Unlock()
		return
	}
	w.ended[sum.ID] = sum.EndedAt
	w.mu.Unlock()
	w.append(sum)
}

// append finds sum's workspace again (by its CWD; the id must still match)
// and writes the index line.
func (w *Workspaces) append(sum protocol.SessionSummary) {
	if sum.WorkspaceID == "" || w.home == "" {
		return
	}
	ws, err := w.find(sum.CWD, w.home)
	if err != nil {
		w.warnOnce("find:"+sum.CWD, "workspaces: ignoring this folder's .rex/", err)
		return
	}
	if ws == nil || ws.ID != sum.WorkspaceID {
		return
	}
	line := workspace.IndexLine{
		SessionID: sum.ID, ShortID: sum.ShortID, Tool: sum.ToolID, Model: sum.ModelID,
		Name: sum.Name, CWD: sum.CWD, StartedAt: sum.StartedAt, EndedAt: sum.EndedAt,
		State: string(sum.State), ResumeID: sum.ResumeID,
	}
	if line.Name == "" {
		line.Name = sum.Slug
	}
	if err := workspace.AppendIndex(ws, line); err != nil {
		w.warnOnce("index:"+ws.Root, "workspaces: cannot write the session index; will keep trying quietly", err)
	}
}

func (w *Workspaces) warnOnce(key, msg string, err error) {
	if _, dup := w.warned.LoadOrStore(key, struct{}{}); dup {
		return
	}
	w.warnings.Add(1)
	slog.Warn(msg, "key", key, "err", err)
}

// Close stops listening to the store and waits for the writer.
func (w *Workspaces) Close() {
	w.once.Do(func() {
		w.unsub()
		close(w.queue)
	})
	<-w.done
}
