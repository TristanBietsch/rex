package boot

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/features/snapshot"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// SnapshotCaptureTimeout is the longest one capture may run. A capture that
// takes longer is abandoned and its turn is not undoable.
const SnapshotCaptureTimeout = 10 * time.Second

// TurnSnapshotsEnabled reads the turn_snapshots feature switch from
// config.yaml. Called per Undo/Redo. On a read error it uses the default
// (off).
func TurnSnapshotsEnabled() bool {
	st, _, err := settings.LoadDefault()
	if err != nil {
		slog.Warn("daemon: settings load failed for turn_snapshots; using the default", "err", err)
	}
	return settings.FeatureOn(st, "turn_snapshots")
}

// Snapshots captures a session's folder at turn boundaries: on a session's
// transition into working (turn start) and out of it (turn end). It is
// inert while the flag is off: the store subscription only tracks session
// states and cwds in memory, and no goroutine, subprocess or file exists.
//
// Captures never run on the store event's goroutine (often a PTY reader):
// the event enqueues a job on its folder's queue, and one worker goroutine
// per folder drains it in order, then exits. A failed capture is logged and
// never changes a session's state.
type Snapshots struct {
	ctx     context.Context
	store   *snapshot.Store
	timeout time.Duration
	on      atomic.Bool
	unsub   func()
	wg      sync.WaitGroup

	mu     sync.Mutex
	states map[string]protocol.State
	cwds   map[string]string
	queues map[string]*snapQueue
}

type snapJob struct {
	session string
	phase   snapshot.Phase // "" = mark the session removed
}

type snapQueue struct {
	jobs    []snapJob
	running bool
}

// NewSnapshots subscribes to sessions and returns the recorder. Call
// SetEnabled with the initial setting and Close when done.
func NewSnapshots(ctx context.Context, store *snapshot.Store, sessions *state.Store) *Snapshots {
	s := &Snapshots{
		ctx: ctx, store: store, timeout: SnapshotCaptureTimeout,
		states: map[string]protocol.State{}, cwds: map[string]string{}, queues: map[string]*snapQueue{},
	}
	for _, sum := range sessions.Snapshot() {
		s.states[sum.ID], s.cwds[sum.ID] = sum.State, sum.CWD
	}
	s.unsub = sessions.Subscribe(func(e state.Event) { s.onEvent(sessions, e) })
	return s
}

// SetEnabled switches capturing on or off. Jobs already queued still run.
func (s *Snapshots) SetEnabled(on bool) {
	slog.Info("daemon: turn snapshots", "enabled", on)
	s.on.Store(on)
}

// Applier is the WatchSettings applier that follows turn_snapshots.
func (s *Snapshots) Applier() func(*settings.Store) {
	return func(st *settings.Store) { s.SetEnabled(settings.FeatureOn(st, "turn_snapshots")) }
}

// Close stops listening and waits for running captures (the daemon's
// context, already canceled, cuts them short).
func (s *Snapshots) Close() {
	s.unsub()
	s.wg.Wait()
}

// onEvent runs on the store's goroutine: memory only, no I/O.
func (s *Snapshots) onEvent(sessions *state.Store, e state.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	prev := s.states[e.SessionID]
	switch e.Kind {
	case state.EventAdded:
		if e.Summary == nil {
			return
		}
		s.states[e.SessionID], s.cwds[e.SessionID] = e.Summary.State, e.Summary.CWD
		if e.Summary.State == protocol.StateWorking {
			s.enqueueLocked(e.SessionID, snapshot.PhaseStart)
		}
	case state.EventUpdated:
		if e.NewState == nil {
			return
		}
		now := *e.NewState
		s.states[e.SessionID] = now
		if _, ok := s.cwds[e.SessionID]; !ok {
			if sess, ok := sessions.Get(e.SessionID); ok {
				s.cwds[e.SessionID] = sess.Summary().CWD
			}
		}
		switch {
		case now == protocol.StateWorking && prev != protocol.StateWorking:
			s.enqueueLocked(e.SessionID, snapshot.PhaseStart)
		case prev == protocol.StateWorking && now != protocol.StateWorking:
			s.enqueueLocked(e.SessionID, snapshot.PhaseEnd)
		}
	case state.EventRemoved:
		s.enqueueLocked(e.SessionID, "")
		delete(s.states, e.SessionID)
		// cwds keeps the entry until the removal job has run (see run).
	}
}

func (s *Snapshots) enqueueLocked(session string, phase snapshot.Phase) {
	if !s.on.Load() {
		if phase == "" {
			delete(s.cwds, session)
		}
		return
	}
	cwd, ok := s.cwds[session]
	if !ok {
		return
	}
	q := s.queues[cwd]
	if q == nil {
		q = &snapQueue{}
		s.queues[cwd] = q
	}
	q.jobs = append(q.jobs, snapJob{session: session, phase: phase})
	if q.running {
		return
	}
	q.running = true
	s.wg.Add(1)
	go s.run(cwd, q)
}

// run drains one folder's queue in order and exits when it is empty.
func (s *Snapshots) run(cwd string, q *snapQueue) {
	defer s.wg.Done()
	for {
		s.mu.Lock()
		if len(q.jobs) == 0 {
			q.running = false
			delete(s.queues, cwd)
			s.mu.Unlock()
			return
		}
		j := q.jobs[0]
		q.jobs = q.jobs[1:]
		s.mu.Unlock()
		s.do(cwd, j)
		if j.phase == "" {
			s.mu.Lock()
			delete(s.cwds, j.session)
			s.mu.Unlock()
		}
	}
}

func (s *Snapshots) do(cwd string, j snapJob) {
	if j.phase == "" {
		if err := s.store.MarkRemoved(cwd, j.session); err != nil {
			slog.Warn("snapshots: mark removed failed", "session", j.session, "err", err)
		}
		return
	}
	ctx, cancel := context.WithTimeout(s.ctx, s.timeout)
	defer cancel()
	_, err := s.store.Capture(ctx, cwd, j.session, j.phase)
	switch {
	case err == nil:
	case errors.Is(err, snapshot.ErrNotGitWorktree), errors.Is(err, snapshot.ErrNoGit):
		slog.Debug("snapshots: skipped", "session", j.session, "phase", j.phase, "reason", err)
	case s.ctx.Err() != nil:
		// Shutting down: not a failure of the turn.
	default:
		slog.Warn("snapshots: capture failed; the turn is not undoable", "session", j.session, "phase", j.phase, "err", err)
		if j.phase == snapshot.PhaseStart {
			if err := s.store.MarkMissed(cwd, j.session); err != nil {
				slog.Warn("snapshots: record missed capture failed", "session", j.session, "err", err)
			}
		}
	}
}
