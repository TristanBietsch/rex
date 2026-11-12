package loop

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tristanbietsch/rex/internal/features/overview"
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

// Enable loads the persisted loops (every one that was running is paused: a
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
		ru := &run{st: st}
		if st.State == protocol.LoopRunning {
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

type run struct {
	st State
	// timer fires the max_minutes cap.
	timer *time.Timer
	// sawWorking: the session worked since the last prompt, so the next
	// needs_input is a turn end.
	sawWorking bool
	// kick: the session is idle now by assumption (a resumed loop): the next
	// idle moment is a turn end even without a wait reason.
	kick bool
	// held: the user typed; no prompt is sent until the agent has worked and
	// gone idle again (workedSinceInput).
	held, workedSinceInput bool
	// checking: a check command runs; checkSeq identifies it.
	checking    bool
	checkSeq    int
	checkCancel context.CancelFunc
	// inputSeen is the input counter when the turn end was evaluated.
	inputSeen uint64
}

type actor struct {
	r      *Runner
	ctx    context.Context
	cancel context.CancelFunc
	q      *queue
	done   chan struct{}
	wg     sync.WaitGroup
	unsub  func()
	// spawning is above 0 while a loop's session is being started: its
	// events arrive before its id is known, so they are all queued (and
	// ignored later if they belong to no loop).
	spawning atomic.Int32

	// Owned by the actor goroutine.
	runs      map[string]*run
	bySession map[string]*run
}

func (a *actor) loop() {
	defer close(a.done)
	defer a.q.close()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-a.q.wake:
			for {
				f := a.q.pop()
				if f == nil {
					break
				}
				f()
				if a.ctx.Err() != nil {
					return
				}
			}
		}
	}
}

// call runs f on the actor and waits for its result.
func (a *actor) call(f func() (protocol.LoopSummary, error)) (protocol.LoopSummary, error) {
	type res struct {
		s   protocol.LoopSummary
		err error
	}
	ch := make(chan res, 1)
	if !a.q.push(func() { s, err := f(); ch <- res{s, err} }) {
		return protocol.LoopSummary{}, ErrOff
	}
	select {
	case v := <-ch:
		return v.s, v.err
	case <-a.done:
		return protocol.LoopSummary{}, ErrOff
	}
}

// onStoreEvent runs on the store's goroutines: it only queues.
func (a *actor) onStoreEvent(e Event) {
	a.r.inputMu.Lock()
	watched := a.r.watched[e.SessionID]
	a.r.inputMu.Unlock()
	if !watched && a.spawning.Load() == 0 {
		return
	}
	a.q.push(func() { a.onEvent(e) })
}

// --- starting, stopping, pausing ---

func (a *actor) newID() string {
	for {
		var b [4]byte
		if _, err := rand.Read(b[:]); err != nil {
			panic(fmt.Errorf("crypto/rand failed: %w", err))
		}
		id := "loop-" + hex.EncodeToString(b[:])
		if _, taken := a.runs[id]; !taken {
			return id
		}
	}
}

func (a *actor) live() []*run {
	var out []*run
	for _, ru := range a.runs {
		if !ru.st.State.Terminal() {
			out = append(out, ru)
		}
	}
	return out
}

func (a *actor) now() time.Time { return a.r.cfg.Now().UTC() }

func (a *actor) start(def Def) (protocol.LoopSummary, error) {
	live := a.live()
	if len(live) >= a.r.cfg.MaxLoops {
		return protocol.LoopSummary{}, badInput(MsgTooMany)
	}
	ru := &run{st: State{
		ID: a.newID(), Def: def, State: protocol.LoopRunning, Iteration: 1,
		StartedAt: a.now(), UpdatedAt: a.now(),
	}}
	for _, o := range live {
		if o.st.Def.CWD == def.CWD {
			ru.st.Notice = "another loop runs in this folder (" + o.st.ID + "): they may edit the same files"
		}
	}
	if err := writeState(a.r.cfg.StateDir, ru.st); err != nil {
		return protocol.LoopSummary{}, &Error{Kind: KindBadInput, Msg: err.Error()}
	}
	a.spawning.Add(1)
	id, err := a.r.cfg.Host.Spawn(a.ctx, SpawnRequest{
		LoopID: ru.st.ID, Iter: 1,
		Slug: def.Name + "-" + strings.TrimPrefix(ru.st.ID, "loop-"), Title: "loop: " + def.Name,
		Tool: def.Tool, Model: def.Model, Effort: def.Effort, CWD: def.CWD, Prompt: def.Prompt,
	})
	a.spawning.Add(-1)
	if err != nil {
		removeState(a.r.cfg.StateDir, ru.st.ID)
		return protocol.LoopSummary{}, &Error{Kind: KindSpawn, Msg: err.Error(), Err: err}
	}
	ru.st.SessionID = id
	ru.sawWorking = false
	a.runs[ru.st.ID] = ru
	a.bySession[id] = ru
	a.r.watch(id, true)
	a.armTimer(ru)
	a.logf(ru, "started %s: tool %s, model %s, folder %s, session %s", def.Name, def.Tool, def.Model, def.CWD, id)
	a.commit(ru)
	return ru.st.Summary(), nil
}

func (a *actor) armTimer(ru *run) {
	if ru.timer != nil {
		ru.timer.Stop()
	}
	limit := time.Duration(ru.st.Def.Until.MaxMinutes) * a.r.cfg.MinuteUnit
	left := limit - a.now().Sub(ru.st.StartedAt)
	id := ru.st.ID
	fire := func() {
		a.q.push(func() {
			if cur := a.runs[id]; cur != nil && !cur.st.State.Terminal() {
				a.finish(cur, protocol.LoopCapped, protocol.StopMaxMinutes,
					fmt.Sprintf("capped: max_minutes (%d) reached; the session keeps running", cur.st.Def.Until.MaxMinutes))
			}
		})
	}
	if left <= 0 {
		left = 0
	}
	ru.timer = time.AfterFunc(left, fire)
}

// commit persists ru, refreshes the snapshot and tells clients.
func (a *actor) commit(ru *run) {
	ru.st.UpdatedAt = a.now()
	if err := writeState(a.r.cfg.StateDir, ru.st); err != nil {
		slog.Warn("loop: could not persist state", "id", ru.st.ID, "err", err)
	}
	sum := ru.st.Summary()
	a.r.setSnap(sum)
	a.r.cfg.Host.Publish(sum)
}

func (a *actor) logf(ru *run, format string, args ...any) {
	line := fmt.Sprintf("%s [iter %d] %s\n", a.now().Format(time.RFC3339), ru.st.Iteration, fmt.Sprintf(format, args...))
	a.appendLog(ru, []byte(line))
}

func (a *actor) appendLog(ru *run, b []byte) {
	if err := os.MkdirAll(a.r.cfg.StateDir, dirPerm); err != nil {
		slog.Warn("loop: log dir", "err", err)
		return
	}
	f, err := os.OpenFile(LogPath(a.r.cfg.StateDir, ru.st.ID), os.O_APPEND|os.O_CREATE|os.O_WRONLY, filePerm)
	if err != nil {
		slog.Warn("loop: open log", "id", ru.st.ID, "err", err)
		return
	}
	defer f.Close()
	if _, err := f.Write(b); err != nil {
		slog.Warn("loop: write log", "id", ru.st.ID, "err", err)
	}
}

func (a *actor) endRun(ru *run) {
	if ru.timer != nil {
		ru.timer.Stop()
	}
	if ru.checkCancel != nil {
		ru.checkCancel()
	}
	ru.checking = false
}

// finish ends a loop for good.
func (a *actor) finish(ru *run, state protocol.LoopState, reason, notice string) {
	if ru.st.State.Terminal() {
		return
	}
	a.endRun(ru)
	ru.st.State, ru.st.PauseReason, ru.st.StopReason = state, "", reason
	if notice != "" {
		ru.st.Notice = notice
	}
	a.r.watch(ru.st.SessionID, false)
	delete(a.bySession, ru.st.SessionID)
	a.logf(ru, "%s (%s)%s", state, reason, noticeSuffix(notice))
	a.commit(ru)
}

func noticeSuffix(n string) string {
	if n == "" {
		return ""
	}
	return ": " + n
}

// pause pauses a live loop. It keeps a stronger pause (user, restart) over a
// weaker one (the loop waits for you anyway).
func (a *actor) pause(ru *run, reason string) {
	if ru.st.State.Terminal() {
		return
	}
	if ru.st.State == protocol.LoopPaused && (ru.st.PauseReason == reason || !autoReason(ru.st.PauseReason)) {
		return
	}
	if ru.checkCancel != nil {
		ru.checkCancel()
	}
	ru.checking = false
	ru.checkSeq++
	ru.st.State, ru.st.PauseReason = protocol.LoopPaused, reason
	a.logf(ru, "paused: %s", reason)
	a.commit(ru)
}

func (a *actor) unpause(ru *run) {
	if ru.st.State != protocol.LoopPaused {
		return
	}
	ru.st.State, ru.st.PauseReason = protocol.LoopRunning, ""
	a.logf(ru, "running again")
	a.commit(ru)
}

func (a *actor) pauseAll(reason string) {
	for _, ru := range a.runs {
		if ru.st.State == protocol.LoopRunning || (ru.st.State == protocol.LoopPaused && autoReason(ru.st.PauseReason)) {
			ru.st.State = protocol.LoopRunning // let pause take the new reason
			a.pause(ru, reason)
		}
		a.endRun(ru)
	}
}

// autoReason: the loop continues by itself once the cause is gone.
func autoReason(reason string) bool {
	return reason == protocol.PauseNeedsInput || reason == protocol.PauseUserInput
}

func (a *actor) pauseByUser(ru *run) (protocol.LoopSummary, error) {
	if ru.st.State.Terminal() {
		return protocol.LoopSummary{}, &Error{Kind: KindBadState, Msg: "loop already ended"}
	}
	if ru.st.State == protocol.LoopPaused && !autoReason(ru.st.PauseReason) {
		return protocol.LoopSummary{}, &Error{Kind: KindBadState, Msg: "loop is already paused"}
	}
	if ru.st.State == protocol.LoopPaused { // an automatic pause becomes a user pause
		ru.st.State = protocol.LoopRunning
	}
	a.pause(ru, protocol.PauseUser)
	return ru.st.Summary(), nil
}

func (a *actor) stopByUser(ru *run) (protocol.LoopSummary, error) {
	if ru.st.State.Terminal() {
		return protocol.LoopSummary{}, &Error{Kind: KindBadState, Msg: "loop already ended"}
	}
	a.finish(ru, protocol.LoopStopped, protocol.StopByUser, "stopped; the session keeps running")
	return ru.st.Summary(), nil
}

func (a *actor) resumeByUser(ru *run) (protocol.LoopSummary, error) {
	if ru.st.State != protocol.LoopPaused {
		return protocol.LoopSummary{}, &Error{Kind: KindBadState, Msg: "loop is not paused"}
	}
	host := a.r.cfg.Host
	info, ok := host.Session(ru.st.SessionID)
	if !ok {
		return protocol.LoopSummary{}, &Error{Kind: KindBadState, Msg: "the loop's session is gone; stop the loop"}
	}
	if ended(info.State) {
		if err := host.EnsureLive(a.ctx, ru.st.SessionID); err != nil {
			return protocol.LoopSummary{}, &Error{Kind: KindBadState, Msg: "the loop's session cannot be resumed: " + err.Error()}
		}
		info, _ = host.Session(ru.st.SessionID)
	}
	ru.held, ru.workedSinceInput = false, false
	// A working session ends its turn by the normal rules; an idle or
	// restarting one is taken to be at a turn end already.
	ru.sawWorking, ru.kick = info.State == protocol.StateWorking, info.State != protocol.StateWorking
	a.r.watch(ru.st.SessionID, true)
	a.bySession[ru.st.SessionID] = ru
	if ru.timer == nil || ru.st.PauseReason == protocol.PauseRestart || ru.st.PauseReason == protocol.PauseOff {
		a.armTimer(ru)
	}
	a.unpause(ru)
	a.evaluate(ru)
	return ru.st.Summary(), nil
}

// --- reacting to the session ---

func ended(st protocol.State) bool {
	return st == protocol.StateDone || st == protocol.StateFailed || st == protocol.StateCrashed
}

func (a *actor) onUserInput(sessionID string) {
	ru := a.bySession[sessionID]
	if ru == nil || ru.st.State.Terminal() {
		return
	}
	info, _ := a.r.cfg.Host.Session(sessionID)
	ru.held = true
	// Typing while the agent works is answered by the turn that ends later;
	// typing at an idle prompt needs the agent to work first.
	ru.workedSinceInput = info.State == protocol.StateWorking
	ru.kick = false
	if ru.st.State == protocol.LoopRunning {
		a.pause(ru, protocol.PauseUserInput)
	}
}

func (a *actor) onEvent(e Event) {
	ru := a.bySession[e.SessionID]
	if ru == nil || ru.st.State.Terminal() {
		return
	}
	switch {
	case e.Removed:
		a.finish(ru, protocol.LoopStopped, protocol.StopByUser, "stopped: the session was removed")
		return
	case e.State == protocol.StateWorking:
		ru.sawWorking = true
		ru.kick = false
		if ru.held {
			ru.workedSinceInput = true
		}
	case ended(e.State):
		if ru.st.State == protocol.LoopRunning || autoReason(ru.st.PauseReason) {
			ru.st.State = protocol.LoopRunning
			a.pause(ru, protocol.PauseSessionEnded)
		}
		return
	}
	if e.State == protocol.StateNeedsInput || e.WaitReason != nil {
		a.evaluate(ru)
	}
}

// active: the loop reacts to turn ends (running, or waiting for you in a way
// that ends by itself).
func active(ru *run) bool {
	return ru.st.State == protocol.LoopRunning ||
		(ru.st.State == protocol.LoopPaused && autoReason(ru.st.PauseReason))
}

// evaluate looks at the session and, if it just ended a turn, handles it.
// A permission or question prompt pauses the loop and never gets a key.
func (a *actor) evaluate(ru *run) {
	if !active(ru) || ru.checking {
		return
	}
	if !ru.sawWorking && !ru.kick {
		return
	}
	host := a.r.cfg.Host
	info, ok := host.Session(ru.st.SessionID)
	if !ok || info.State != protocol.StateNeedsInput {
		return
	}
	switch {
	case info.WaitReason == protocol.WaitInput:
		a.waitForYou(ru)
		return
	case info.WaitReason == protocol.WaitReview:
	case host.ReportsReason(info.ToolID) && !ru.kick:
		return // the hook that says why has not arrived yet
	}
	if !ru.kick && overview.IsQuestion(info.LastLine) {
		a.waitForYou(ru)
		return
	}
	a.turnEnd(ru)
}

// waitForYou pauses a running loop at a prompt that needs you. A loop that
// is already paused keeps its reason (your typing hold stands).
func (a *actor) waitForYou(ru *run) {
	if ru.st.State == protocol.LoopRunning {
		a.pause(ru, protocol.PauseNeedsInput)
	}
}

func (a *actor) turnEnd(ru *run) {
	ru.sawWorking, ru.kick = false, false
	if ru.held {
		if !ru.workedSinceInput {
			// You typed but the agent has not worked since: stay out.
			a.pause(ru, protocol.PauseUserInput)
			return
		}
		ru.held, ru.workedSinceInput = false, false
	}
	a.unpause(ru)
	ru.inputSeen = a.r.inputCount(ru.st.SessionID)
	def := ru.st.Def
	a.logf(ru, "turn ended")
	if def.Until.Marker != "" && def.Until.MatchMarker(a.r.cfg.Host.Output(ru.st.SessionID)) {
		a.finish(ru, protocol.LoopDone, protocol.StopMarker, "")
		return
	}
	if len(def.Until.Check) > 0 {
		a.runCheck(ru)
		return
	}
	a.afterChecks(ru)
}

func (a *actor) runCheck(ru *run) {
	ru.checking = true
	ru.checkSeq++
	seq := ru.checkSeq
	ctx, cancel := context.WithCancel(a.ctx)
	ru.checkCancel = cancel
	def := ru.st.Def
	id := ru.st.ID
	a.logf(ru, "check: %s (timeout %s)", strings.Join(def.Until.Check, " "), def.Until.CheckTimeout)
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		defer cancel()
		res := RunCheck(ctx, def.Until.Check, def.CWD, def.Until.CheckTimeout)
		a.q.push(func() { a.checkDone(id, seq, res) })
	}()
}

func (a *actor) checkDone(id string, seq int, res CheckResult) {
	ru := a.runs[id]
	if ru == nil || ru.checkSeq != seq || !ru.checking {
		return // paused, stopped or superseded while it ran
	}
	ru.checking = false
	ru.checkCancel = nil
	switch {
	case res.Err != nil:
		a.logf(ru, "check could not run: %v", res.Err)
		ru.st.LastCheckExit = nil
		a.finish(ru, protocol.LoopFailed, protocol.StopCheckError, "the check command could not run: "+res.Err.Error())
		return
	case res.TimedOut:
		ru.st.LastCheckExit = nil
		a.logf(ru, "check killed at its timeout (%s)", ru.st.Def.Until.CheckTimeout)
	default:
		code := res.ExitCode
		ru.st.LastCheckExit = &code
		a.logf(ru, "check exited %d", code)
	}
	if len(res.Output) > 0 {
		a.appendLog(ru, indent(res.Output))
		if res.Truncated {
			a.appendLog(ru, []byte("    ... output truncated at 64 KiB\n"))
		}
	}
	if res.Passed() {
		a.finish(ru, protocol.LoopDone, protocol.StopCheckPassed, "")
		return
	}
	a.afterChecks(ru)
}

func indent(b []byte) []byte {
	var sb strings.Builder
	for line := range strings.SplitSeq(strings.TrimRight(string(b), "\n"), "\n") {
		sb.WriteString("    ")
		sb.WriteString(line)
		sb.WriteByte('\n')
	}
	return []byte(sb.String())
}

// afterChecks applies the caps and sends the next prompt.
func (a *actor) afterChecks(ru *run) {
	def := ru.st.Def
	if ru.st.Iteration >= def.Until.MaxIterations {
		a.finish(ru, protocol.LoopCapped, protocol.StopMaxIterations,
			fmt.Sprintf("capped: max_iterations (%d) reached; the session keeps running", def.Until.MaxIterations))
		return
	}
	host := a.r.cfg.Host
	// The user may have typed while the check ran or just now.
	if a.r.inputCount(ru.st.SessionID) != ru.inputSeen || !active(ru) {
		ru.held, ru.workedSinceInput = true, false
		if ru.st.State == protocol.LoopRunning {
			a.pause(ru, protocol.PauseUserInput)
		}
		return
	}
	if err := host.Send(ru.st.SessionID, def.FollowupPrompt); err != nil {
		a.logf(ru, "could not send the prompt: %v", err)
		ru.st.Notice = "could not send the prompt: " + err.Error()
		a.pause(ru, protocol.PauseSendFailed)
		return
	}
	ru.st.Iteration++
	ru.sawWorking = false
	host.SetSessionLoop(ru.st.SessionID, ru.st.ID, ru.st.Iteration)
	a.logf(ru, "sent the follow-up prompt")
	a.commit(ru)
}
