package loop

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// DefaultMaxLoops is the most loops that may be live (running or paused) at
// once.
const DefaultMaxLoops = 2

// Config is a Runner's dependencies.
type Config struct {
	Host Host
	// StateDir holds <id>.json and <id>.log (the loop state dir).
	StateDir string
	// LoadDef reads and validates a loop file by name. It is called only for
	// a StartLoop, so with the feature off no loop file is ever read.
	LoadDef func(name string) (Def, error)
	// MaxLoops caps live loops (0 = DefaultMaxLoops).
	MaxLoops int
	// Now is the clock (nil = time.Now).
	Now func() time.Time
	// SessionCtx is the context loop sessions are spawned and resumed under:
	// the daemon's serve context, the same one ordinary sessions get. It must
	// outlive the runner, so that switching the feature off never kills an
	// agent. nil means context.Background().
	SessionCtx context.Context
	// MinuteUnit is what max_minutes counts in (0 = a minute). Tests shrink it.
	MinuteUnit time.Duration
}

// Runner drives repeat loops. It is inert until Enable: no goroutine, no
// subscription, no file read or written. All loop state is owned by one
// goroutine (the actor); store events, intents and check results reach it as
// queued closures, so nothing in a loop needs a lock.
type Runner struct {
	cfg Config

	lifeMu sync.Mutex
	act    *actor

	snapMu sync.RWMutex
	snaps  map[string]protocol.LoopSummary

	// inputMu guards the user-input bookkeeping, which the server's
	// connection goroutines write without going through the actor.
	inputMu sync.Mutex
	watched map[string]bool // session id -> belongs to a live loop
	inputs  map[string]uint64
}

// NewRunner returns a disabled Runner.
func NewRunner(cfg Config) *Runner {
	if cfg.MaxLoops <= 0 {
		cfg.MaxLoops = DefaultMaxLoops
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.SessionCtx == nil {
		cfg.SessionCtx = context.Background()
	}
	if cfg.MinuteUnit <= 0 {
		cfg.MinuteUnit = time.Minute
	}
	return &Runner{
		cfg: cfg, snaps: map[string]protocol.LoopSummary{},
		watched: map[string]bool{}, inputs: map[string]uint64{},
	}
}

// Enabled reports whether the runner is running.
func (r *Runner) Enabled() bool {
	r.lifeMu.Lock()
	defer r.lifeMu.Unlock()
	return r.act != nil
}

// Enable loads the persisted loops (every one that was running, or waiting
// for you in a way that ends by itself, is paused as daemon_restart: a
// restart never resumes a loop by itself) and starts the runner. A second
// call does nothing.
func (r *Runner) Enable() error {
	r.lifeMu.Lock()
	defer r.lifeMu.Unlock()
	if r.act != nil {
		return nil
	}
	states, err := loadStates(r.cfg.StateDir, r.cfg.Now())
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	a := &actor{
		r: r, ctx: ctx, cancel: cancel, q: newQueue(), done: make(chan struct{}),
		runs: map[string]*run{}, bySession: map[string]*run{},
	}
	for _, st := range states {
		ru := &run{st: st, elapsed: time.Duration(st.ElapsedNS)}
		if st.State == protocol.LoopRunning || (st.State == protocol.LoopPaused && autoReason(st.PauseReason)) {
			ru.st.State, ru.st.PauseReason = protocol.LoopPaused, protocol.PauseRestart
			ru.st.UpdatedAt = r.cfg.Now().UTC()
			if err := writeState(r.cfg.StateDir, ru.st); err != nil {
				slog.Warn("loop: could not persist paused state", "id", st.ID, "err", err)
			}
		}
		a.runs[st.ID] = ru
		if !st.State.Terminal() {
			a.bySession[st.SessionID] = ru
			r.watch(st.SessionID, true)
		}
		r.setSnap(ru.st.Summary())
	}
	a.unsub = r.cfg.Host.Subscribe(a.onStoreEvent)
	go a.loop()
	r.act = a
	slog.Info("loop: runner enabled", "loops", len(states))
	return nil
}

// Disable pauses every live loop (reason feature_off) and stops the runner.
func (r *Runner) Disable() { r.stop(protocol.PauseOff) }

// Close pauses every live loop (reason daemon_restart) and stops the runner.
func (r *Runner) Close() { r.stop(protocol.PauseRestart) }

func (r *Runner) stop(reason string) {
	r.lifeMu.Lock()
	a := r.act
	r.act = nil
	r.lifeMu.Unlock()
	if a == nil {
		return
	}
	a.unsub()
	// Pause inside the actor, then end it.
	ch := make(chan struct{})
	if a.q.push(func() { a.pauseAll(reason); close(ch) }) {
		select {
		case <-ch:
		case <-a.done:
		}
	}
	a.cancel()
	<-a.done
	a.wg.Wait()
	r.snapMu.Lock()
	r.snaps = map[string]protocol.LoopSummary{}
	r.snapMu.Unlock()
	r.inputMu.Lock()
	r.watched, r.inputs = map[string]bool{}, map[string]uint64{}
	r.inputMu.Unlock()
	slog.Info("loop: runner stopped", "reason", reason)
}

func (r *Runner) actor() (*actor, error) {
	r.lifeMu.Lock()
	defer r.lifeMu.Unlock()
	if r.act == nil {
		return nil, ErrOff
	}
	return r.act, nil
}

// Summaries lists the loops, newest first. Empty while disabled.
func (r *Runner) Summaries() []protocol.LoopSummary {
	r.snapMu.RLock()
	out := make([]protocol.LoopSummary, 0, len(r.snaps))
	for _, s := range r.snaps {
		out = append(out, s)
	}
	r.snapMu.RUnlock()
	slices.SortFunc(out, func(a, b protocol.LoopSummary) int {
		if c := b.StartedAt.Compare(a.StartedAt); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	return out
}

func (r *Runner) setSnap(s protocol.LoopSummary) {
	r.snapMu.Lock()
	r.snaps[s.ID] = s
	r.snapMu.Unlock()
}

func (r *Runner) watch(sessionID string, on bool) {
	if sessionID == "" {
		return
	}
	r.inputMu.Lock()
	defer r.inputMu.Unlock()
	if on {
		r.watched[sessionID] = true
	} else {
		delete(r.watched, sessionID)
		delete(r.inputs, sessionID)
	}
}

func (r *Runner) inputCount(sessionID string) uint64 {
	r.inputMu.Lock()
	defer r.inputMu.Unlock()
	return r.inputs[sessionID]
}

// UserInput tells the runner a client typed into a session. For a session in
// a live loop it holds back re-sends until the agent has been idle again, so
// the runner never interleaves keystrokes with yours. It does not block and
// is cheap for any other session.
func (r *Runner) UserInput(sessionID string) {
	r.inputMu.Lock()
	if !r.watched[sessionID] {
		r.inputMu.Unlock()
		return
	}
	r.inputs[sessionID]++
	r.inputMu.Unlock()
	if a, err := r.actor(); err == nil {
		a.q.push(func() { a.onUserInput(sessionID) })
	}
}

// Start starts the loop called req.Name.
func (r *Runner) Start(req protocol.StartLoop) (protocol.LoopSummary, error) {
	a, err := r.actor()
	if err != nil {
		return protocol.LoopSummary{}, err
	}
	if !ValidName(req.Name) {
		return protocol.LoopSummary{}, badInput(fmt.Sprintf("loop name %q is invalid", req.Name))
	}
	def, err := r.cfg.LoadDef(req.Name)
	if err != nil {
		return protocol.LoopSummary{}, badInput(err.Error())
	}
	if !r.cfg.Host.ReportsReason(def.Tool) {
		return protocol.LoopSummary{}, badInput(NoReasonMessage(def.Tool))
	}
	cwd := req.CWD
	if cwd == "" {
		cwd = def.CWD
	}
	if cwd == "" {
		return protocol.LoopSummary{}, badInput("no folder: set cwd in the loop file or pass a folder")
	}
	if !filepath.IsAbs(cwd) || slices.Contains(strings.FieldsFunc(cwd, func(c rune) bool { return c == '/' || c == '\\' }), "..") || strings.ContainsRune(cwd, 0) {
		return protocol.LoopSummary{}, badInput("folder must be an absolute path without \"..\"")
	}
	if fi, err := os.Stat(cwd); err != nil || !fi.IsDir() {
		return protocol.LoopSummary{}, badInput(fmt.Sprintf("folder %s does not exist", cwd))
	}
	def.CWD = filepath.Clean(cwd)
	return a.call(func() (protocol.LoopSummary, error) { return a.start(def) })
}

// Pause pauses a running loop.
func (r *Runner) Pause(id string) (protocol.LoopSummary, error) {
	return r.control(id, (*actor).pauseByUser)
}

// Resume continues a paused loop.
func (r *Runner) Resume(id string) (protocol.LoopSummary, error) {
	return r.control(id, (*actor).resumeByUser)
}

// Stop ends a loop for good.
func (r *Runner) Stop(id string) (protocol.LoopSummary, error) {
	return r.control(id, (*actor).stopByUser)
}

func (r *Runner) control(id string, f func(*actor, *run) (protocol.LoopSummary, error)) (protocol.LoopSummary, error) {
	a, err := r.actor()
	if err != nil {
		return protocol.LoopSummary{}, err
	}
	if !protocol.ValidLoopID(id) {
		return protocol.LoopSummary{}, errNotFound
	}
	return a.call(func() (protocol.LoopSummary, error) {
		ru := a.runs[id]
		if ru == nil {
			return protocol.LoopSummary{}, errNotFound
		}
		return f(a, ru)
	})
}

// --- the actor ---

type queue struct {
	mu     sync.Mutex
	items  []func()
	closed bool
	wake   chan struct{}
}

func newQueue() *queue { return &queue{wake: make(chan struct{}, 1)} }

func (q *queue) push(f func()) bool {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return false
	}
	q.items = append(q.items, f)
	q.mu.Unlock()
	select {
	case q.wake <- struct{}{}:
	default:
	}
	return true
}

func (q *queue) pop() func() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.items) == 0 {
		return nil
	}
	f := q.items[0]
	q.items[0] = nil
	q.items = q.items[1:]
	return f
}

func (q *queue) close() {
	q.mu.Lock()
	q.closed, q.items = true, nil
	q.mu.Unlock()
}
