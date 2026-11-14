package boot

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/features/audit"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
	"github.com/tristanbietsch/rex/internal/workspace"
)

// auditQueueSize bounds the hand-off from the store callback to the writer.
// The teardown path writes any record a full queue drops.
const auditQueueSize = 256

// Audit writes the audit trail: one record when a session first reaches a
// terminal state, and one in the teardown path if that has not happened yet.
// It is inert while the audit_trail setting is off: no file is opened, no
// workspace is looked up and the prompt is not kept.
//
// A workspace session (its folder is inside a workspace and the workspaces
// setting is on) writes to <root>/.rex/audit.jsonl; if that cannot be
// written, or any other session ends, the record goes to the global file.
type Audit struct {
	global  string
	home    string
	persist func(*state.Session)

	on, full, ws atomic.Bool

	// find is workspace.Find; tests replace it to count lookups.
	find func(cwd, home string) (*workspace.Workspace, error)
	now  func() time.Time

	warned sync.Map // path -> struct{}: one notice per file per daemon run

	queue chan auditJob
	store *state.Store
	unsub func()
	done  chan struct{}
}

// AuditConfig is NewAudit's input.
type AuditConfig struct {
	// StateDir is the daemon's state directory; the global audit file lives
	// in it.
	StateDir string
	// Home bounds workspace discovery ("" disables it).
	Home string
	// Persist saves a session's meta after its record was written, so a
	// restart does not write it again. nil: not persisted.
	Persist func(*state.Session)
}

// NewAudit builds the audit writer for store and starts listening to it.
// It starts off: call Configure (and use Applier) with the settings. Close
// stops it after the queued records are written.
func NewAudit(cfg AuditConfig, store *state.Store) *Audit {
	a := &Audit{
		global:  filepath.Join(cfg.StateDir, paths.AuditFileName),
		home:    cfg.Home,
		persist: cfg.Persist,
		find:    workspace.Find,
		now:     time.Now,
		queue:   make(chan auditJob, auditQueueSize),
		store:   store,
		done:    make(chan struct{}),
	}
	// The callback only queues the id (the store forbids I/O in it).
	a.unsub = store.Subscribe(func(e state.Event) {
		if !a.on.Load() || e.Kind != state.EventUpdated || e.NewState == nil || !state.IsTerminal(*e.NewState) {
			return
		}
		// The summary is copied now, so the record shows the state of the
		// first terminal transition even if the session moves on.
		sess, ok := a.store.Get(e.SessionID)
		if !ok {
			return
		}
		select {
		case a.queue <- auditJob{sess: sess, sum: sess.Summary()}:
		default:
			slog.Warn("audit: queue full; the record is written when the session is cleared", "session", e.SessionID)
		}
	})
	go a.run()
	return a
}

// run writes the queued records until Close closes the queue, so a record
// queued just before shutdown is still written.
func (a *Audit) run() {
	defer close(a.done)
	for job := range a.queue {
		_ = a.end(job.sess, job.sum) // logged once; the teardown path retries
	}
}

// PersistMeta returns an AuditConfig.Persist that saves a session's meta.json
// under stateDir, logging a failure.
func PersistMeta(stateDir string) func(*state.Session) {
	return func(sess *state.Session) {
		if err := state.WriteMeta(stateDir, sess); err != nil {
			slog.Error("audit: persist meta failed", "session", sess.ID, "err", err)
		}
	}
}

// AuditSettingIDs are the settings Configure reads: the SettingsHub
// subscription for Applier must name every one, or an edit of it would not
// apply until the daemon restarts.
var AuditSettingIDs = []string{"audit_trail", "audit_full_prompt", "workspaces"}

// Configure applies the audit_trail, audit_full_prompt and workspaces
// settings.
func (a *Audit) Configure(s *settings.Store) {
	a.on.Store(settings.FeatureOn(s, "audit_trail"))
	a.full.Store(settings.FeatureOn(s, "audit_full_prompt"))
	a.ws.Store(settings.FeatureOn(s, "workspaces"))
}

// Applier is the SettingsHub subscriber that follows the audit settings.
func (a *Audit) Applier() func(*settings.Store) { return a.Configure }

// Begin keeps the first line of a new session's prompt (and the whole prompt
// when audit_full_prompt is on) for its record.
func (a *Audit) Begin(sess *state.Session, prompt string) {
	if !a.on.Load() || prompt == "" {
		return
	}
	full := ""
	if a.full.Load() {
		full = audit.TrimPrompt(prompt)
	}
	sess.SetAuditText(audit.FirstLine(prompt), full)
}

// auditJob is one queued record: the session and its summary at the
// terminal transition.
type auditJob struct {
	sess *state.Session
	sum  protocol.SessionSummary
}

// End writes sess's record unless its current run already has one. A write
// that fails is logged once per file per daemon run and tried again at the
// next call (the teardown path calls it before a session is deleted).
func (a *Audit) End(sess *state.Session) error { return a.end(sess, sess.Summary()) }

func (a *Audit) end(sess *state.Session, sum protocol.SessionSummary) error {
	if !a.on.Load() {
		return nil
	}
	wrote, err := sess.AuditOnce(func() error { return a.write(sess, sum) })
	if err != nil {
		a.warnOnce(a.global, err)
		return err
	}
	if wrote && a.persist != nil {
		a.persist(sess)
	}
	return nil
}

func (a *Audit) write(sess *state.Session, sum protocol.SessionSummary) error {
	first, full := sess.AuditText()
	wsID, path := "", a.global
	if a.ws.Load() && a.home != "" {
		w, err := a.find(sum.CWD, a.home)
		switch {
		case err != nil:
			a.warnOnce("find:"+sum.CWD, err)
		case w != nil:
			wsID, path = w.ID, filepath.Join(w.Root, workspace.DirName, workspace.AuditFileName)
		}
	}
	rec := audit.FromSummary(sum, wsID, first, full, a.full.Load(), a.now())
	err := audit.Append(path, rec)
	if err != nil && path != a.global {
		// A workspace file that cannot be written (read-only, unsafe) must
		// not lose the record.
		a.warnOnce(path, err)
		err = audit.Append(a.global, rec)
	}
	return err
}

// warnOnce logs err for key the first time only.
func (a *Audit) warnOnce(key string, err error) {
	if _, dup := a.warned.LoadOrStore(key, struct{}{}); dup {
		return
	}
	level := slog.LevelWarn
	if errors.Is(err, audit.ErrNotRegular) || errors.Is(err, workspace.ErrUnsafe) {
		level = slog.LevelError
	}
	slog.Log(context.Background(), level, "audit: cannot write the audit trail; will keep trying quietly", "path", key, "err", err)
}

// Close stops listening to the store and waits for the writer.
func (a *Audit) Close() {
	a.unsub()
	// Drain what is queued, then stop the writer.
	close(a.queue)
	<-a.done
}
