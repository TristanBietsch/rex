package loop

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tristanbietsch/rex/internal/features/overview"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// run is one loop in the actor: its persisted State plus what only the
// running daemon knows.
type run struct {
	st State
	// timer fires the max_minutes cap; it runs only while the loop runs.
	timer *time.Timer
	// elapsed is running time so far, not counting pauses or downtime;
	// runSince is when the current running stretch began (zero while paused).
	elapsed  time.Duration
	runSince time.Time
	// sawWorking: the session worked since the last prompt, so the next
	// finished turn is a turn end.
	sawWorking bool
	// kick: a resumed loop whose session was idle, not working: an existing
	// finished-turn reason counts as a turn end. It never replaces the reason.
	kick bool
	// held: the user typed; no prompt is sent until the agent has worked and
	// gone idle again (workedSinceInput).
	held, workedSinceInput bool
	// checking: a check command runs; checkSeq identifies it.
	checking    bool
	checkSeq    int
	checkCancel context.CancelFunc
	// dirty: while the check ran the session worked or asked for input, so
	// the turn the check judged is no longer the latest and nothing is sent
	// for it.
	dirty bool
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

// ranFor is the loop's running time so far.
func (a *actor) ranFor(ru *run) time.Duration {
	if ru.runSince.IsZero() {
		return ru.elapsed
	}
	return ru.elapsed + max(a.now().Sub(ru.runSince), 0)
}

// stopClock folds the current running stretch into elapsed and stops the
// cap timer: a paused loop is not using its minutes.
func (a *actor) stopClock(ru *run) {
	if !ru.runSince.IsZero() {
		ru.elapsed = a.ranFor(ru)
		ru.runSince = time.Time{}
	}
	if ru.timer != nil {
		ru.timer.Stop()
		ru.timer = nil
	}
}

// startClock begins a running stretch and arms the max_minutes cap for the
// minutes that are left.
func (a *actor) startClock(ru *run) {
	ru.runSince = a.now()
	if ru.timer != nil {
		ru.timer.Stop()
	}
	limit := time.Duration(ru.st.Def.Until.MaxMinutes) * a.r.cfg.MinuteUnit
	left := max(limit-ru.elapsed, 0)
	id := ru.st.ID
	ru.timer = time.AfterFunc(left, func() {
		a.q.push(func() {
			if cur := a.runs[id]; cur != nil && !cur.st.State.Terminal() && cur.st.State == protocol.LoopRunning {
				a.finish(cur, protocol.LoopCapped, protocol.StopMaxMinutes,
					fmt.Sprintf("capped: max_minutes (%d) of running time reached; the session keeps running", cur.st.Def.Until.MaxMinutes))
			}
		})
	})
}

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
	// The session outlives the runner: it gets the daemon's context, not the
	// actor's, so switching the feature off never kills an agent.
	id, err := a.r.cfg.Host.Spawn(a.r.cfg.SessionCtx, SpawnRequest{
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
	a.runs[ru.st.ID] = ru
	a.bySession[id] = ru
	a.r.watch(id, true)
	a.startClock(ru)
	a.logf(ru, "started %s: tool %s, model %s, folder %s, session %s", def.Name, def.Tool, def.Model, def.CWD, id)
	a.commit(ru)
	return ru.st.Summary(), nil
}

// commit persists ru, refreshes the snapshot and tells clients.
func (a *actor) commit(ru *run) {
	ru.st.UpdatedAt = a.now()
	ru.st.ElapsedNS = int64(a.ranFor(ru))
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
	a.stopClock(ru)
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
	a.stopClock(ru)
	ru.st.State, ru.st.PauseReason = protocol.LoopPaused, reason
	a.logf(ru, "paused: %s", reason)
	a.commit(ru)
}

func (a *actor) unpause(ru *run) {
	if ru.st.State != protocol.LoopPaused {
		return
	}
	ru.st.State, ru.st.PauseReason = protocol.LoopRunning, ""
	a.startClock(ru)
	a.logf(ru, "running again")
	a.commit(ru)
}

func (a *actor) pauseAll(reason string) {
	for _, ru := range a.runs {
		if ru.st.State == protocol.LoopRunning || (ru.st.State == protocol.LoopPaused && autoReason(ru.st.PauseReason)) {
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
	if !host.ReportsReason(ru.st.Def.Tool) {
		return protocol.LoopSummary{}, badInput(NoReasonMessage(ru.st.Def.Tool))
	}
	info, ok := host.Session(ru.st.SessionID)
	if !ok {
		return protocol.LoopSummary{}, &Error{Kind: KindBadState, Msg: "the loop's session is gone; stop the loop"}
	}
	if ended(info.State) {
		if err := host.EnsureLive(a.r.cfg.SessionCtx, ru.st.SessionID); err != nil {
			return protocol.LoopSummary{}, &Error{Kind: KindBadState, Msg: "the loop's session cannot be resumed: " + err.Error()}
		}
		info, _ = host.Session(ru.st.SessionID)
	}
	ru.held, ru.workedSinceInput, ru.dirty = false, false, false
	// A working session ends its turn by the normal rules. An idle one is
	// not sent to straight away: only a finished-turn reason it already
	// shows (and no question) counts as a turn end; otherwise the loop waits
	// for the agent's next real turn end.
	ru.sawWorking, ru.kick = info.State == protocol.StateWorking, info.State != protocol.StateWorking
	a.r.watch(ru.st.SessionID, true)
	a.bySession[ru.st.SessionID] = ru
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
		if ru.checking {
			ru.dirty = true
		}
	case ended(e.State):
		if ru.st.State == protocol.LoopRunning || autoReason(ru.st.PauseReason) {
			a.pause(ru, protocol.PauseSessionEnded)
		}
		return
	}
	if ru.checking && e.WaitReason != nil && *e.WaitReason == protocol.WaitInput {
		ru.dirty = true
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

// atTurnEnd classifies the session right now: ready is true only when it
// sits at needs_input with the finished-turn reason and a last line that is
// not a question. needsYou is true when it asks for input or a question.
func (a *actor) atTurnEnd(ru *run) (info protocol.SessionSummary, ready, needsYou bool) {
	info, ok := a.r.cfg.Host.Session(ru.st.SessionID)
	if !ok || info.State != protocol.StateNeedsInput {
		return info, false, false
	}
	switch info.WaitReason {
	case protocol.WaitInput:
		return info, false, true
	case protocol.WaitReview:
		if overview.IsQuestion(info.LastLine) {
			return info, false, true
		}
		return info, true, false
	}
	// No reason yet (the hook has not arrived): not a turn end.
	return info, false, false
}

// evaluate looks at the session and, if it just ended a turn, handles it. A
// permission or question prompt pauses the loop and never gets a key; and a
// session that does not say why it waits is never treated as finished.
func (a *actor) evaluate(ru *run) {
	if !active(ru) || ru.checking {
		return
	}
	_, ready, needsYou := a.atTurnEnd(ru)
	switch {
	case needsYou:
		a.waitForYou(ru)
	case ready && (ru.sawWorking || ru.kick):
		a.turnEnd(ru)
	}
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
	ru.dirty = false
	def := ru.st.Def
	a.logf(ru, "turn ended")
	if def.MarkerMatches(a.r.cfg.Host.Output(ru.st.SessionID)) {
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
		if res.Leaked {
			a.logf(ru, "the check left a child process holding its output; it was killed")
		}
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
	if ru.dirty {
		// The session worked or asked while the check ran: the turn the check
		// judged is not the latest. Send nothing; act at its next turn end.
		ru.dirty = false
		a.logf(ru, "the session moved on during the check; waiting for its next turn end")
		a.commit(ru)
		a.evaluate(ru)
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

// afterChecks applies the caps and sends the next prompt, but only if the
// session still sits at a clean turn end right now and the user has not
// typed since.
func (a *actor) afterChecks(ru *run) {
	def := ru.st.Def
	if ru.st.Iteration >= def.Until.MaxIterations {
		a.finish(ru, protocol.LoopCapped, protocol.StopMaxIterations,
			fmt.Sprintf("capped: max_iterations (%d) reached; the session keeps running", def.Until.MaxIterations))
		return
	}
	host := a.r.cfg.Host
	id := ru.st.SessionID
	// The user may have typed while the check ran or just now.
	if a.r.inputCount(id) != ru.inputSeen || !active(ru) {
		ru.held, ru.workedSinceInput = true, false
		if ru.st.State == protocol.LoopRunning {
			a.pause(ru, protocol.PauseUserInput)
		}
		return
	}
	// Re-read the session: it must be waiting with a finished turn, not at a
	// prompt and not working.
	info, ready, needsYou := a.atTurnEnd(ru)
	if !ready {
		a.logf(ru, "not sending: the session is %s%s", info.State, reasonSuffix(info.WaitReason))
		switch {
		case needsYou:
			a.waitForYou(ru)
		case info.State == protocol.StateWorking:
			ru.sawWorking = true
		}
		return
	}
	seen := ru.inputSeen
	sent, err := host.Send(id, def.FollowupPrompt, func() bool { return a.r.inputCount(id) == seen })
	if err != nil {
		a.logf(ru, "could not send the prompt: %v", err)
		ru.st.Notice = "could not send the prompt: " + err.Error()
		a.pause(ru, protocol.PauseSendFailed)
		return
	}
	if !sent {
		// The user typed between the check above and the send.
		ru.held, ru.workedSinceInput = true, false
		if ru.st.State == protocol.LoopRunning {
			a.pause(ru, protocol.PauseUserInput)
		}
		return
	}
	ru.st.Iteration++
	ru.sawWorking = false
	host.SetSessionLoop(id, ru.st.ID, ru.st.Iteration)
	a.logf(ru, "sent the follow-up prompt")
	a.commit(ru)
}

func reasonSuffix(reason string) string {
	if reason == "" {
		return ""
	}
	return " (" + reason + ")"
}
