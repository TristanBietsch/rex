package loop

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/features/overview"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// fakeHost is a daemon stand-in: sessions are plain structs, and the test
// plays the agent by changing their state (the synthetic hooks).
type fakeHost struct {
	mu        sync.Mutex
	sessions  map[string]*protocol.SessionSummary
	output    map[string]string
	sent      []string
	spawns    []SpawnRequest
	loopIter  map[string]int
	published []protocol.LoopSummary
	subs      []func(Event)
	subCalls  int
	live      []string
	claude    bool
	sendErr   error
	spawnErr  error
	liveErr   error
	nextID    int
	// sentAt is each session's summary at the moment of every Send.
	sentAt []protocol.SessionSummary
	// killed lists sessions whose context ended (a real session dies with
	// the context it was spawned under).
	// beforeGuard and gap are test hooks run inside Send (see Send).
	beforeGuard, gap func()
	typed            int
	killed           []string
	liveCtxs         []context.Context
	spawnCtxs        []context.Context
}

func newFakeHost() *fakeHost {
	return &fakeHost{
		sessions: map[string]*protocol.SessionSummary{}, output: map[string]string{},
		loopIter: map[string]int{}, claude: true,
	}
}

func (h *fakeHost) Spawn(ctx context.Context, req SpawnRequest) (string, error) {
	h.mu.Lock()
	if h.spawnErr != nil {
		h.mu.Unlock()
		return "", h.spawnErr
	}
	h.nextID++
	id := "sess" + strconv.Itoa(h.nextID)
	h.sessions[id] = &protocol.SessionSummary{ID: id, ToolID: req.Tool, State: protocol.StateWorking}
	h.spawns = append(h.spawns, req)
	h.spawnCtxs = append(h.spawnCtxs, ctx)
	subs := append([]func(Event){}, h.subs...)
	h.mu.Unlock()
	// The agent dies with the context it was spawned under.
	context.AfterFunc(ctx, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.killed = append(h.killed, id)
		if sess, ok := h.sessions[id]; ok {
			sess.State = protocol.StateCrashed
		}
	})
	// The session starts working before Spawn returns, like a real one.
	for _, fn := range subs {
		fn(Event{SessionID: id, State: protocol.StateWorking})
	}
	return id, nil
}

// Send is the real send in miniature: the guard is asked before the text and
// again after a gap, just before Enter. beforeGuard and gap let a test change
// the session (or call UserInput) at those two moments. typed counts texts
// that were typed but not submitted.
func (h *fakeHost) Send(id, text string, guard func() bool) (bool, error) {
	h.mu.Lock()
	sendErr, before, gap := h.sendErr, h.beforeGuard, h.gap
	h.mu.Unlock()
	if sendErr != nil {
		return false, sendErr
	}
	if before != nil {
		before()
	}
	if guard != nil && !guard() {
		return false, nil
	}
	h.mu.Lock()
	h.typed++
	h.mu.Unlock()
	if gap != nil {
		gap()
	}
	if guard != nil && !guard() {
		return false, nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sent = append(h.sent, id+":"+text)
	if s, ok := h.sessions[id]; ok {
		h.sentAt = append(h.sentAt, *s)
	}
	return true, nil
}

// requireSendsAtTurnEnd checks the one safety property of every Send: the
// session was waiting with a finished-turn reason, and no question, at that
// moment. newRig runs it at the end of every test.
func (h *fakeHost) requireSendsAtTurnEnd(t *testing.T) {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	for i, s := range h.sentAt {
		require.Equal(t, protocol.StateNeedsInput, s.State, "send %d: session state", i)
		require.Equal(t, protocol.WaitReview, s.WaitReason, "send %d: wait reason", i)
		require.False(t, overview.IsQuestion(s.LastLine), "send %d: question", i)
	}
}

func (h *fakeHost) Session(id string) (protocol.SessionSummary, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	s, ok := h.sessions[id]
	if !ok {
		return protocol.SessionSummary{}, false
	}
	return *s, true
}

func (h *fakeHost) Output(id string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.output[id]
}

func (h *fakeHost) ReportsReason(string) bool { return h.claude }

func (h *fakeHost) EnsureLive(ctx context.Context, id string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.liveCtxs = append(h.liveCtxs, ctx)
	if h.liveErr != nil {
		return h.liveErr
	}
	h.live = append(h.live, id)
	h.sessions[id].State = protocol.StateNeedsInput
	h.sessions[id].WaitReason = ""
	return nil
}

func (h *fakeHost) SetSessionLoop(id, loopID string, iter int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.loopIter[id] = iter
	_ = loopID
}

func (h *fakeHost) Subscribe(fn func(Event)) func() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.subCalls++
	h.subs = append(h.subs, fn)
	idx := len(h.subs) - 1
	return func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.subs[idx] = func(Event) {}
	}
}

func (h *fakeHost) Publish(s protocol.LoopSummary) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.published = append(h.published, s)
}

func (h *fakeHost) emit(e Event) {
	h.mu.Lock()
	subs := append([]func(Event){}, h.subs...)
	h.mu.Unlock()
	for _, fn := range subs {
		fn(e)
	}
}

// work makes the agent start a turn.
func (h *fakeHost) work(id string) {
	h.mu.Lock()
	h.sessions[id].State, h.sessions[id].WaitReason = protocol.StateWorking, ""
	h.mu.Unlock()
	h.emit(Event{SessionID: id, State: protocol.StateWorking})
}

// idle makes the agent stop: the state first, then the hook's wait reason, as
// the daemon reports them.
func (h *fakeHost) idle(id, reason, lastLine string) {
	h.mu.Lock()
	h.sessions[id].State, h.sessions[id].LastLine = protocol.StateNeedsInput, lastLine
	h.mu.Unlock()
	h.emit(Event{SessionID: id, State: protocol.StateNeedsInput})
	if reason != "" {
		h.mu.Lock()
		h.sessions[id].WaitReason = reason
		h.mu.Unlock()
		h.emit(Event{SessionID: id, WaitReason: &reason})
	}
}

func (h *fakeHost) spawnCtx() context.Context {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.spawnCtxs[0]
}

func (h *fakeHost) killedSessions() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.killed...)
}

func (h *fakeHost) sentCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.sent)
}

func (h *fakeHost) setOutput(id, text string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.output[id] = text
}

type rig struct {
	t       *testing.T
	h       *fakeHost
	r       *Runner
	dir     string
	defs    map[string]Def
	loaded  []string
	sessCtx context.Context
	endSess context.CancelFunc
	clockMu sync.Mutex
	clock   time.Time
}

func (rg *rig) now() time.Time {
	rg.clockMu.Lock()
	defer rg.clockMu.Unlock()
	return rg.clock
}

func (rg *rig) advance(d time.Duration) {
	rg.clockMu.Lock()
	defer rg.clockMu.Unlock()
	rg.clock = rg.clock.Add(d)
}

func newRig(t *testing.T) *rig {
	t.Helper()
	t.Setenv("REX_LOOP_HELPER", "1")
	rg := &rig{t: t, h: newFakeHost(), dir: filepath.Join(t.TempDir(), "loops"), defs: map[string]Def{},
		clock: time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC)}
	// The daemon's context: sessions live under it, never under the runner.
	rg.sessCtx, rg.endSess = context.WithCancel(context.Background())
	t.Cleanup(rg.endSess)
	rg.r = rg.newRunner()
	t.Cleanup(func() { rg.r.Close() })
	t.Cleanup(func() { rg.h.requireSendsAtTurnEnd(t) })
	return rg
}

func (rg *rig) newRunner() *Runner {
	return NewRunner(Config{
		Host: rg.h, StateDir: rg.dir, MinuteUnit: time.Minute, SessionCtx: rg.sessCtx, Now: rg.now,
		LoadDef: func(name string) (Def, error) {
			rg.loaded = append(rg.loaded, name)
			d, ok := rg.defs[name]
			if !ok {
				return Def{}, errors.New("no such loop file")
			}
			return d, nil
		},
	})
}

func (rg *rig) enable() {
	rg.t.Helper()
	require.NoError(rg.t, rg.r.Enable())
}

// def registers a loop whose check runs the test binary in helper mode.
func (rg *rig) def(name string, checkMode []string, extra string) Def {
	rg.t.Helper()
	until := "  marker: 'ALL DONE'\n"
	if checkMode != nil {
		argv := append([]string{os.Args[0], "-test.run=^TestCheckHelper$", "--"}, checkMode...)
		b, _ := json.Marshal(argv)
		until = "  check: " + string(b) + "\n"
	}
	d, err := Parse([]byte("name: "+name+"\nkind: repeat\ntool: claude\nmodel: sonnet\nprompt: start it\nfollowup_prompt: go on\nuntil:\n"+until+extra), name)
	require.NoError(rg.t, err)
	rg.defs[name] = d
	return d
}

func (rg *rig) start(name string) protocol.LoopSummary {
	rg.t.Helper()
	s, err := rg.r.Start(protocol.StartLoop{Name: name, CWD: rg.t.TempDir()})
	require.NoError(rg.t, err)
	return s
}

func (rg *rig) summary(id string) protocol.LoopSummary {
	for _, s := range rg.r.Summaries() {
		if s.ID == id {
			return s
		}
	}
	rg.t.Fatalf("no loop %s", id)
	return protocol.LoopSummary{}
}

func (rg *rig) waitState(id string, want protocol.LoopState) protocol.LoopSummary {
	rg.t.Helper()
	require.Eventually(rg.t, func() bool { return rg.summary(id).State == want }, 15*time.Second, 10*time.Millisecond,
		"loop %s state: want %s, have %+v", id, want, rg.summary(id))
	return rg.summary(id)
}

func (rg *rig) waitSent(n int) {
	rg.t.Helper()
	require.Eventually(rg.t, func() bool { return rg.h.sentCount() >= n }, 15*time.Second, 10*time.Millisecond, "sent %d, want %d", rg.h.sentCount(), n)
}

// turn plays one agent turn: work, then stop with a finished-turn hook.
func (rg *rig) turn(sess string) {
	rg.h.work(sess)
	rg.h.idle(sess, protocol.WaitReview, "done for now.")
}

func TestLoop_CompletesWhenCheckPasses(t *testing.T) {
	rg := newRig(t)
	rg.enable()
	counter := filepath.Join(t.TempDir(), "n")
	rg.def("fix", []string{"count", counter, "3"}, "")
	l := rg.start("fix")
	require.Equal(t, protocol.LoopRunning, l.State)
	require.Equal(t, 1, l.Iteration)
	require.Len(t, rg.h.spawns, 1)
	require.Equal(t, "start it", rg.h.spawns[0].Prompt)
	require.Equal(t, l.ID, rg.h.spawns[0].LoopID)

	// Turn 1 ends, the check fails (count 1), the follow-up goes out.
	rg.h.idle("sess1", protocol.WaitReview, "turn 1")
	rg.waitSent(1)
	require.Equal(t, "sess1:go on", rg.h.sent[0])
	require.Equal(t, 2, rg.h.loopIter["sess1"])
	// Turn 2: check fails again (count 2).
	rg.turn("sess1")
	rg.waitSent(2)
	// Turn 3: check passes.
	rg.turn("sess1")
	done := rg.waitState(l.ID, protocol.LoopDone)
	require.Equal(t, protocol.StopCheckPassed, done.StopReason)
	require.Equal(t, 3, done.Iteration)
	require.NotNil(t, done.LastCheckExit)
	require.Equal(t, 0, *done.LastCheckExit)
	require.Equal(t, 2, rg.h.sentCount(), "no prompt after the loop is done")
	rg.h.requireSendsAtTurnEnd(t)

	logb, err := os.ReadFile(LogPath(rg.dir, l.ID))
	require.NoError(t, err)
	require.Contains(t, string(logb), "check exited 0")
	require.Contains(t, string(logb), "out line", "check output goes to the log")
}

func TestLoop_StopsAtMaxIterations(t *testing.T) {
	rg := newRig(t)
	rg.enable()
	rg.def("cap", []string{"exit", "1"}, "  max_iterations: 2\n")
	l := rg.start("cap")
	rg.h.idle("sess1", protocol.WaitReview, "t1")
	rg.waitSent(1)
	rg.turn("sess1")
	capped := rg.waitState(l.ID, protocol.LoopCapped)
	require.Equal(t, protocol.StopMaxIterations, capped.StopReason)
	require.Equal(t, 2, capped.Iteration)
	require.Contains(t, capped.Notice, "max_iterations")
	require.Equal(t, 1, rg.h.sentCount(), "the cap sends no third prompt")
	require.Equal(t, 1, *capped.LastCheckExit)
}

func TestLoop_MarkerNeedsAWholeLine(t *testing.T) {
	rg := newRig(t)
	rg.enable()
	rg.def("mark", nil, "")
	l := rg.start("mark")
	// The agent quotes the marker inside a sentence: not a stop.
	rg.h.setOutput("sess1", "I will print ALL DONE when finished\nworking on it")
	rg.h.idle("sess1", protocol.WaitReview, "t1")
	rg.waitSent(1)
	require.Equal(t, protocol.LoopRunning, rg.summary(l.ID).State)
	// Now the marker stands alone.
	rg.h.work("sess1")
	rg.h.setOutput("sess1", "all green\n  ALL DONE  \n")
	rg.h.idle("sess1", protocol.WaitReview, "ALL DONE")
	done := rg.waitState(l.ID, protocol.LoopDone)
	require.Equal(t, protocol.StopMarker, done.StopReason)
	require.Equal(t, 1, rg.h.sentCount())
}

func TestLoop_HungCheckIsKilledAndLoopContinues(t *testing.T) {
	rg := newRig(t)
	rg.enable()
	d := rg.def("hang", []string{"hang"}, "")
	d.Until.CheckTimeout = 300 * time.Millisecond
	rg.defs["hang"] = d
	l := rg.start("hang")
	rg.h.idle("sess1", protocol.WaitReview, "t1")
	rg.waitSent(1) // the check timed out, so it did not pass, so the loop went on
	s := rg.summary(l.ID)
	require.Equal(t, protocol.LoopRunning, s.State)
	require.Nil(t, s.LastCheckExit)
	logb, _ := os.ReadFile(LogPath(rg.dir, l.ID))
	require.Contains(t, string(logb), "killed at its timeout")
}

func TestLoop_CheckThatCannotRunFailsTheLoop(t *testing.T) {
	rg := newRig(t)
	rg.enable()
	d := rg.def("bad", []string{"exit", "0"}, "")
	d.Until.Check = []string{"rex-no-such-command-xyz"}
	rg.defs["bad"] = d
	l := rg.start("bad")
	rg.h.idle("sess1", protocol.WaitReview, "t1")
	f := rg.waitState(l.ID, protocol.LoopFailed)
	require.Equal(t, protocol.StopCheckError, f.StopReason)
	require.Zero(t, rg.h.sentCount())
}

// A permission or question prompt pauses the loop and sends no keystrokes;
// once the user answered and the agent finishes, the loop goes on.
func TestLoop_NeedsInputPausesAndSendsNothing(t *testing.T) {
	rg := newRig(t)
	rg.enable()
	rg.def("ask", []string{"exit", "1"}, "")
	l := rg.start("ask")
	rg.h.work("sess1")
	rg.h.idle("sess1", protocol.WaitInput, "Allow Bash(rm -rf)?")
	p := rg.waitState(l.ID, protocol.LoopPaused)
	require.Equal(t, protocol.PauseNeedsInput, p.PauseReason)
	time.Sleep(200 * time.Millisecond)
	require.Zero(t, rg.h.sentCount(), "never answer a prompt")
	require.Nil(t, p.LastCheckExit, "the check does not run at a prompt")

	// The user approves (a client keystroke), the agent works and finishes.
	rg.r.UserInput("sess1")
	rg.h.work("sess1")
	rg.h.idle("sess1", protocol.WaitReview, "done.")
	rg.waitSent(1)
	require.Equal(t, protocol.LoopRunning, rg.summary(l.ID).State)
}

func TestLoop_QuestionInTextPauses(t *testing.T) {
	rg := newRig(t)
	rg.enable()
	rg.def("q", []string{"exit", "1"}, "")
	l := rg.start("q")
	rg.h.work("sess1")
	rg.h.idle("sess1", protocol.WaitReview, "Should I delete the old tests?")
	p := rg.waitState(l.ID, protocol.LoopPaused)
	require.Equal(t, protocol.PauseNeedsInput, p.PauseReason)
	time.Sleep(150 * time.Millisecond)
	require.Zero(t, rg.h.sentCount())
}

// Loops are only for agents that say why they wait: for any other tool a
// permission prompt looks just like a finished turn.
func TestLoop_RefusedForAToolThatDoesNotReportWhy(t *testing.T) {
	rg := newRig(t)
	rg.h.claude = false
	rg.enable()
	rg.def("plain", []string{"exit", "1"}, "")
	_, err := rg.r.Start(protocol.StartLoop{Name: "plain", CWD: t.TempDir()})
	var le *Error
	require.ErrorAs(t, err, &le)
	require.Equal(t, KindBadInput, le.Kind)
	require.Equal(t, "loops need an agent that reports why it is waiting (Claude hooks or OSC 7501 status); claude can't, so a loop could answer its permission prompts", le.Msg)
	require.Empty(t, rg.h.spawns, "nothing was spawned")
	require.Empty(t, rg.r.Summaries())
}

// A loop started for a tool that reports why cannot be resumed once that is
// no longer true (a registry change, say).
func TestLoop_ResumeRefusedForAToolThatDoesNotReportWhy(t *testing.T) {
	rg := newRig(t)
	rg.enable()
	rg.def("a", []string{"exit", "1"}, "")
	l := rg.start("a")
	_, err := rg.r.Pause(l.ID)
	require.NoError(t, err)
	rg.h.claude = false
	_, err = rg.r.Resume(l.ID)
	var le *Error
	require.ErrorAs(t, err, &le)
	require.Equal(t, KindBadInput, le.Kind)
	require.Contains(t, le.Msg, "loops need an agent that reports why it is waiting")
	require.Equal(t, protocol.LoopPaused, rg.summary(l.ID).State)
}

// Claude needs its hook: needs_input without a reason is not a turn end yet.
func TestLoop_ClaudeWaitsForTheHook(t *testing.T) {
	rg := newRig(t)
	rg.enable()
	rg.def("hook", []string{"exit", "1"}, "")
	rg.start("hook")
	rg.h.idle("sess1", "", "something")
	time.Sleep(200 * time.Millisecond)
	require.Zero(t, rg.h.sentCount())
	reason := protocol.WaitReview
	rg.h.mu.Lock()
	rg.h.sessions["sess1"].WaitReason = reason
	rg.h.mu.Unlock()
	rg.h.emit(Event{SessionID: "sess1", WaitReason: &reason})
	rg.waitSent(1)
}

// User input during the loop holds re-sends until the agent is idle again.
func TestLoop_UserInputHoldsResends(t *testing.T) {
	rg := newRig(t)
	rg.enable()
	rg.def("typed", []string{"exit", "1"}, "")
	l := rg.start("typed")

	// The user types while the agent works; the turn that ends is theirs too.
	rg.r.UserInput("sess1")
	p := rg.waitState(l.ID, protocol.LoopPaused)
	require.Equal(t, protocol.PauseUserInput, p.PauseReason)
	rg.h.idle("sess1", protocol.WaitReview, "t1")
	time.Sleep(200 * time.Millisecond)
	// The turn after the input ended: the hold is released and the loop goes on.
	rg.waitSent(1)
	require.Equal(t, protocol.LoopRunning, rg.summary(l.ID).State)

	// The user types at the idle prompt without submitting: nothing is sent.
	rg.h.work("sess1")
	rg.h.idle("sess1", protocol.WaitReview, "t2")
	rg.waitSent(2)
	rg.h.work("sess1")
	rg.h.idle("sess1", protocol.WaitReview, "t3")
	rg.h.mu.Lock()
	rg.h.sessions["sess1"].State = protocol.StateNeedsInput
	rg.h.mu.Unlock()
	rg.r.UserInput("sess1")
	p = rg.waitState(l.ID, protocol.LoopPaused)
	require.Equal(t, protocol.PauseUserInput, p.PauseReason)
	rg.h.idle("sess1", protocol.WaitReview, "t3 again")
	time.Sleep(200 * time.Millisecond)
	require.Equal(t, 2, rg.h.sentCount(), "no keystrokes while the user's draft sits at the prompt")
	// They submit: the agent works and ends; the loop continues.
	rg.h.work("sess1")
	rg.h.idle("sess1", protocol.WaitReview, "t4")
	rg.waitSent(3)
}

// Input that lands while a check runs is honored before the send.
func TestLoop_InputDuringCheckHoldsTheSend(t *testing.T) {
	rg := newRig(t)
	rg.enable()
	rg.def("slow", []string{"sleepexit", "400ms", "1"}, "")
	l := rg.start("slow")
	rg.h.idle("sess1", protocol.WaitReview, "t1")
	time.Sleep(100 * time.Millisecond)
	rg.r.UserInput("sess1")
	require.Eventually(t, func() bool { return rg.summary(l.ID).PauseReason == protocol.PauseUserInput }, 5*time.Second, 10*time.Millisecond)
	time.Sleep(600 * time.Millisecond)
	require.Zero(t, rg.h.sentCount())
}

func TestLoop_ThirdConcurrentLoopRefused(t *testing.T) {
	rg := newRig(t)
	rg.enable()
	rg.def("a", []string{"exit", "1"}, "")
	l1 := rg.start("a")
	rg.start("a")
	_, err := rg.r.Start(protocol.StartLoop{Name: "a", CWD: t.TempDir()})
	var le *Error
	require.ErrorAs(t, err, &le)
	require.Equal(t, KindBadInput, le.Kind)
	require.Equal(t, "too many loops", le.Msg)
	require.Len(t, rg.h.spawns, 2, "the refused loop spawned nothing")
	// A finished loop frees its slot.
	_, err = rg.r.Stop(l1.ID)
	require.NoError(t, err)
	rg.start("a")
}

func TestLoop_SameFolderGetsANotice(t *testing.T) {
	rg := newRig(t)
	rg.enable()
	rg.def("a", []string{"exit", "1"}, "")
	cwd := t.TempDir()
	_, err := rg.r.Start(protocol.StartLoop{Name: "a", CWD: cwd})
	require.NoError(t, err)
	l2, err := rg.r.Start(protocol.StartLoop{Name: "a", CWD: cwd})
	require.NoError(t, err)
	require.Contains(t, l2.Notice, "another loop runs in this folder")
}

// A daemon restart leaves every loop paused, never auto-resumed.
func TestLoop_RestartLeavesLoopsPaused(t *testing.T) {
	rg := newRig(t)
	rg.enable()
	rg.def("keep", []string{"exit", "1"}, "")
	l := rg.start("keep")
	rg.h.idle("sess1", protocol.WaitReview, "t1")
	rg.waitSent(1)

	// A clean shutdown pauses.
	rg.r.Close()
	info, err := os.Stat(filepath.Join(rg.dir, l.ID+".json"))
	require.NoError(t, err)
	if info.Mode().Perm() != 0o600 {
		require.Equal(t, os.FileMode(0o666), info.Mode().Perm(), "windows reports 0666")
	}

	// A crash leaves "running" on disk; loading pauses it too.
	b, err := os.ReadFile(filepath.Join(rg.dir, l.ID+".json"))
	require.NoError(t, err)
	var st State
	require.NoError(t, json.Unmarshal(b, &st))
	require.Equal(t, protocol.LoopPaused, st.State)
	st.State, st.PauseReason = protocol.LoopRunning, ""
	require.NoError(t, writeState(rg.dir, st))

	r2 := rg.newRunner()
	t.Cleanup(r2.Close)
	rg.h.sessions["sess1"].State = protocol.StateCrashed
	require.NoError(t, r2.Enable())
	got := r2.Summaries()
	require.Len(t, got, 1)
	require.Equal(t, protocol.LoopPaused, got[0].State)
	require.Equal(t, protocol.PauseRestart, got[0].PauseReason)
	time.Sleep(150 * time.Millisecond)
	require.Equal(t, 1, rg.h.sentCount(), "a restarted loop sends nothing by itself")

	// Resume brings the session back. It is idle but says nothing about why,
	// so the loop does not send into it; it waits for the next real turn end.
	s, err := r2.Resume(l.ID)
	require.NoError(t, err)
	require.Equal(t, protocol.LoopRunning, s.State)
	require.Equal(t, []string{"sess1"}, rg.h.live)
	require.Equal(t, []any{rg.sessCtx}, []any{rg.h.liveCtxs[0]}, "resumed under the daemon's context")
	time.Sleep(200 * time.Millisecond)
	require.Equal(t, 1, rg.h.sentCount())
	rg.turn("sess1")
	require.Eventually(t, func() bool { return rg.h.sentCount() == 2 }, 15*time.Second, 10*time.Millisecond)
	rg.h.requireSendsAtTurnEnd(t)
}

func TestLoop_ResumeFailsWhenSessionCannotComeBack(t *testing.T) {
	rg := newRig(t)
	rg.enable()
	rg.def("keep", []string{"exit", "1"}, "")
	l := rg.start("keep")
	rg.r.Close()
	r2 := rg.newRunner()
	t.Cleanup(r2.Close)
	rg.h.sessions["sess1"].State = protocol.StateCrashed
	rg.h.liveErr = errors.New("cannot be resumed")
	require.NoError(t, r2.Enable())
	_, err := r2.Resume(l.ID)
	var le *Error
	require.ErrorAs(t, err, &le)
	require.Equal(t, KindBadState, le.Kind)
	require.Equal(t, protocol.LoopPaused, r2.Summaries()[0].State)

	delete(rg.h.sessions, "sess1")
	_, err = r2.Resume(l.ID)
	require.ErrorAs(t, err, &le)
	require.Contains(t, le.Msg, "gone")
}

func TestLoop_PauseResumeStop(t *testing.T) {
	rg := newRig(t)
	rg.enable()
	rg.def("ctl", []string{"exit", "1"}, "")
	l := rg.start("ctl")

	s, err := rg.r.Pause(l.ID)
	require.NoError(t, err)
	require.Equal(t, protocol.PauseUser, s.PauseReason)
	_, err = rg.r.Pause(l.ID)
	var le *Error
	require.ErrorAs(t, err, &le)
	require.Equal(t, KindBadState, le.Kind)
	// Paused by the user: a turn end sends nothing.
	rg.h.idle("sess1", protocol.WaitReview, "t1")
	time.Sleep(200 * time.Millisecond)
	require.Zero(t, rg.h.sentCount())

	s, err = rg.r.Resume(l.ID)
	require.NoError(t, err)
	require.Equal(t, protocol.LoopRunning, s.State)
	rg.waitSent(1) // the session is idle: the resume carries on at once
	_, err = rg.r.Resume(l.ID)
	require.ErrorAs(t, err, &le)
	require.Equal(t, KindBadState, le.Kind)

	s, err = rg.r.Stop(l.ID)
	require.NoError(t, err)
	require.Equal(t, protocol.LoopStopped, s.State)
	_, err = rg.r.Stop(l.ID)
	require.ErrorAs(t, err, &le)
	_, err = rg.r.Pause(l.ID)
	require.ErrorAs(t, err, &le)

	_, err = rg.r.Pause("loop-deadbeef")
	require.ErrorAs(t, err, &le)
	require.Equal(t, KindNotFound, le.Kind)
	_, err = rg.r.Stop("../../etc")
	require.ErrorAs(t, err, &le)
	require.Equal(t, KindNotFound, le.Kind)
}

func TestLoop_WallClockCap(t *testing.T) {
	rg := newRig(t)
	rg.r = NewRunner(Config{Host: rg.h, StateDir: rg.dir, MinuteUnit: 100 * time.Millisecond,
		LoadDef: func(string) (Def, error) { return rg.defs["w"], nil }})
	t.Cleanup(rg.r.Close)
	rg.enable()
	rg.def("w", []string{"exit", "1"}, "  max_minutes: 1\n")
	l := rg.start("w")
	capped := rg.waitState(l.ID, protocol.LoopCapped)
	require.Equal(t, protocol.StopMaxMinutes, capped.StopReason)
	require.Contains(t, capped.Notice, "max_minutes")
}

func TestLoop_SessionEventsPauseOrStop(t *testing.T) {
	rg := newRig(t)
	rg.enable()
	rg.def("e", []string{"exit", "1"}, "")
	l := rg.start("e")
	rg.h.emit(Event{SessionID: "sess1", State: protocol.StateCrashed})
	p := rg.waitState(l.ID, protocol.LoopPaused)
	require.Equal(t, protocol.PauseSessionEnded, p.PauseReason)

	l2 := rg.start("e")
	rg.h.emit(Event{SessionID: "sess2", Removed: true})
	s := rg.waitState(l2.ID, protocol.LoopStopped)
	require.Contains(t, s.Notice, "removed")
}

func TestLoop_SendFailurePauses(t *testing.T) {
	rg := newRig(t)
	rg.enable()
	rg.def("s", []string{"exit", "1"}, "")
	l := rg.start("s")
	rg.h.sendErr = errors.New("input buffer full")
	rg.h.idle("sess1", protocol.WaitReview, "t1")
	p := rg.waitState(l.ID, protocol.LoopPaused)
	require.Equal(t, protocol.PauseSendFailed, p.PauseReason)
	require.Contains(t, p.Notice, "input buffer full")
}

func TestLoop_StartRefusals(t *testing.T) {
	rg := newRig(t)
	rg.enable()
	rg.def("ok", []string{"exit", "1"}, "")
	cases := []struct {
		name string
		req  protocol.StartLoop
		want string
	}{
		{"bad name", protocol.StartLoop{Name: "../x", CWD: t.TempDir()}, "invalid"},
		{"missing file", protocol.StartLoop{Name: "nope", CWD: t.TempDir()}, "no such loop file"},
		{"no folder", protocol.StartLoop{Name: "ok"}, "no folder"},
		{"relative folder", protocol.StartLoop{Name: "ok", CWD: "rel"}, "absolute"},
		{"dotdot folder", protocol.StartLoop{Name: "ok", CWD: t.TempDir() + "/../x"}, "absolute"},
		{"missing folder", protocol.StartLoop{Name: "ok", CWD: filepath.Join(t.TempDir(), "gone")}, "does not exist"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := rg.r.Start(tc.req)
			var le *Error
			require.ErrorAs(t, err, &le)
			require.Equal(t, KindBadInput, le.Kind)
			require.Contains(t, le.Msg, tc.want)
		})
	}
	require.Empty(t, rg.h.spawns)

	rg.h.spawnErr = errors.New("too many concurrent sessions (cap=1)")
	_, err := rg.r.Start(protocol.StartLoop{Name: "ok", CWD: t.TempDir()})
	var le *Error
	require.ErrorAs(t, err, &le)
	require.Equal(t, KindSpawn, le.Kind)
	require.ErrorIs(t, err, rg.h.spawnErr)
	entries, _ := os.ReadDir(rg.dir)
	for _, e := range entries {
		require.NotContains(t, e.Name(), ".json", "a failed spawn leaves no state behind")
	}
}

// With the feature off the runner does nothing at all.
func TestRunner_OffIsInert(t *testing.T) {
	rg := newRig(t)
	rg.def("fix", []string{"exit", "1"}, "")
	require.False(t, rg.r.Enabled())
	_, err := rg.r.Start(protocol.StartLoop{Name: "fix", CWD: t.TempDir()})
	require.True(t, IsOff(err))
	require.Equal(t, MsgOff, err.Error())
	for _, f := range []func(string) (protocol.LoopSummary, error){rg.r.Pause, rg.r.Resume, rg.r.Stop} {
		_, err = f("loop-00000000")
		require.True(t, IsOff(err))
	}
	rg.r.UserInput("sess1")
	require.Empty(t, rg.r.Summaries())
	require.Empty(t, rg.loaded, "no loop file is read")
	require.Zero(t, rg.h.subCalls, "no subscription")
	_, statErr := os.Stat(rg.dir)
	require.True(t, os.IsNotExist(statErr), "no state written")
	rg.r.Disable()
	rg.r.Close()
}

func TestRunner_DisablePausesAndRefuses(t *testing.T) {
	rg := newRig(t)
	rg.enable()
	rg.def("fix", []string{"exit", "1"}, "")
	l := rg.start("fix")
	rg.r.Disable()
	require.False(t, rg.r.Enabled())
	_, err := rg.r.Stop(l.ID)
	require.True(t, IsOff(err))
	require.Empty(t, rg.r.Summaries())

	rg.enable()
	got := rg.r.Summaries()
	require.Len(t, got, 1)
	require.Equal(t, protocol.PauseOff, got[0].PauseReason)
	require.NoError(t, rg.r.Enable(), "a second Enable does nothing")
}

func TestLoad_SkipsBadFilesAndPrunesOld(t *testing.T) {
	rg := newRig(t)
	require.NoError(t, os.MkdirAll(rg.dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(rg.dir, "loop-aaaaaaaa.json"), []byte("{not json"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(rg.dir, "loop-bbbbbbbb.json"), []byte(`{"version":1,"id":"loop-cccccccc"}`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(rg.dir, "notes.txt"), []byte("x"), 0o600))
	d := rg.def("old", nil, "")
	old := State{ID: "loop-dddddddd", Def: d, State: protocol.LoopDone, StartedAt: time.Now().Add(-30 * 24 * time.Hour), UpdatedAt: time.Now().Add(-30 * 24 * time.Hour)}
	require.NoError(t, writeState(rg.dir, old))
	require.NoError(t, os.WriteFile(LogPath(rg.dir, old.ID), []byte("x"), 0o600))
	fresh := State{ID: "loop-eeeeeeee", Def: d, State: protocol.LoopDone, StartedAt: time.Now(), UpdatedAt: time.Now()}
	require.NoError(t, writeState(rg.dir, fresh))
	rg.enable()
	got := rg.r.Summaries()
	require.Len(t, got, 1)
	require.Equal(t, "loop-eeeeeeee", got[0].ID)
	require.NoFileExists(t, statePath(rg.dir, old.ID))
	require.NoFileExists(t, LogPath(rg.dir, old.ID))
	require.True(t, strings.HasPrefix(got[0].ID, "loop-"))
}

// Switching the feature off pauses the loops but never kills their agents:
// sessions live under the daemon's context, not the runner's.
func TestRunner_DisableLeavesSessionsAlive(t *testing.T) {
	rg := newRig(t)
	rg.enable()
	rg.def("keep", []string{"exit", "1"}, "")
	l := rg.start("keep")
	require.Same(t, rg.sessCtx, rg.h.spawnCtx(), "spawned under the session context")

	rg.r.Disable()
	require.Empty(t, rg.h.killedSessions(), "the agent is still running")
	sess, _ := rg.h.Session("sess1")
	require.Equal(t, protocol.StateWorking, sess.State)
	rg.enable()
	require.Equal(t, protocol.PauseOff, rg.summary(l.ID).PauseReason)

	// The fake does kill a session whose context ends, so the check above
	// proves something.
	rg.endSess()
	require.Eventually(t, func() bool { return len(rg.h.killedSessions()) == 1 }, 5*time.Second, 10*time.Millisecond)
}

// A permission prompt that appears while the check runs gets no keystroke.
func TestLoop_PromptDuringCheckSendsNothing(t *testing.T) {
	rg := newRig(t)
	rg.enable()
	rg.def("slow", []string{"sleepexit", "400ms", "1"}, "")
	l := rg.start("slow")
	rg.h.idle("sess1", protocol.WaitReview, "t1") // the check starts and takes 400 ms
	time.Sleep(100 * time.Millisecond)
	rg.h.work("sess1")
	rg.h.idle("sess1", protocol.WaitInput, "Allow Bash(rm -rf /)?")
	time.Sleep(700 * time.Millisecond)
	require.Zero(t, rg.h.sentCount(), "never type into a prompt")
	p := rg.waitState(l.ID, protocol.LoopPaused)
	require.Equal(t, protocol.PauseNeedsInput, p.PauseReason)
}

// Even when no event announced it, the state is re-read right before a send.
func TestLoop_SendIsRecheckedAgainstTheSession(t *testing.T) {
	cases := []struct {
		name  string
		state protocol.State
		why   string
		line  string
	}{
		{"working", protocol.StateWorking, "", "busy"},
		{"input prompt", protocol.StateNeedsInput, protocol.WaitInput, "Allow it?"},
		{"no reason", protocol.StateNeedsInput, "", "hm"},
		{"a question", protocol.StateNeedsInput, protocol.WaitReview, "Which one do you want?"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rg := newRig(t)
			rg.enable()
			rg.def("slow", []string{"sleepexit", "300ms", "1"}, "")
			rg.start("slow")
			rg.h.idle("sess1", protocol.WaitReview, "t1")
			time.Sleep(80 * time.Millisecond)
			// The state changes under the runner with no event.
			rg.h.mu.Lock()
			s := rg.h.sessions["sess1"]
			s.State, s.WaitReason, s.LastLine = tc.state, tc.why, tc.line
			rg.h.mu.Unlock()
			time.Sleep(600 * time.Millisecond)
			require.Zero(t, rg.h.sentCount())
		})
	}
}

// Resuming at a question, or at an idle session that gives no reason, sends
// nothing.
func TestLoop_ResumeDoesNotSendIntoAQuestionOrAnUnexplainedPrompt(t *testing.T) {
	rg := newRig(t)
	rg.enable()
	rg.def("r", []string{"exit", "1"}, "")
	l := rg.start("r")
	_, err := rg.r.Pause(l.ID)
	require.NoError(t, err)
	rg.h.mu.Lock()
	s := rg.h.sessions["sess1"]
	s.State, s.WaitReason, s.LastLine = protocol.StateNeedsInput, protocol.WaitReview, "Shall I continue?"
	rg.h.mu.Unlock()
	got, err := rg.r.Resume(l.ID)
	require.NoError(t, err)
	require.Equal(t, protocol.LoopPaused, got.State)
	require.Equal(t, protocol.PauseNeedsInput, got.PauseReason)
	time.Sleep(150 * time.Millisecond)
	require.Zero(t, rg.h.sentCount())

	// An idle session with no reason: the loop runs but waits.
	l2 := rg.start("r")
	_, err = rg.r.Pause(l2.ID)
	require.NoError(t, err)
	rg.h.mu.Lock()
	s = rg.h.sessions["sess2"]
	s.State, s.WaitReason, s.LastLine = protocol.StateNeedsInput, "", "ready"
	rg.h.mu.Unlock()
	got, err = rg.r.Resume(l2.ID)
	require.NoError(t, err)
	require.Equal(t, protocol.LoopRunning, got.State)
	time.Sleep(150 * time.Millisecond)
	require.Zero(t, rg.h.sentCount())
}

// A loop that was waiting for you in a way that ends by itself must not
// resume by itself after a restart.
func TestLoad_AutoPausedLoopsStayPausedAfterARestart(t *testing.T) {
	for _, reason := range []string{protocol.PauseNeedsInput, protocol.PauseUserInput} {
		t.Run(reason, func(t *testing.T) {
			rg := newRig(t)
			d := rg.def("a", []string{"exit", "1"}, "")
			rg.h.sessions["sess1"] = &protocol.SessionSummary{ID: "sess1", ToolID: "claude", State: protocol.StateNeedsInput, WaitReason: protocol.WaitReview}
			st := State{ID: "loop-0000000a", Def: d, SessionID: "sess1", State: protocol.LoopPaused, PauseReason: reason,
				Iteration: 1, StartedAt: rg.now(), UpdatedAt: rg.now()}
			require.NoError(t, writeState(rg.dir, st))
			rg.enable()
			got := rg.summary("loop-0000000a")
			require.Equal(t, protocol.LoopPaused, got.State)
			require.Equal(t, protocol.PauseRestart, got.PauseReason)
			// The session finishing a turn does not restart the loop.
			rg.h.work("sess1")
			rg.h.idle("sess1", protocol.WaitReview, "done")
			time.Sleep(200 * time.Millisecond)
			require.Zero(t, rg.h.sentCount())
			require.Equal(t, protocol.PauseRestart, rg.summary("loop-0000000a").PauseReason)
		})
	}
}

// A prompt echo that the terminal soft-wrapped must not complete the loop.
func TestLoop_WrappedPromptEchoDoesNotCompleteTheLoop(t *testing.T) {
	rg := newRig(t)
	rg.enable()
	d := rg.def("w", nil, "")
	d.Until.Marker = "<promise>DONE</promise>"
	d.Prompt = "Fix the build and print <promise>DONE</promise> when green"
	d.FollowupPrompt = d.Prompt
	require.NoError(t, d.Compile())
	rg.defs["w"] = d
	l := rg.start("w")
	rg.h.setOutput("sess1", "> Fix the build and print\n  <promise>DONE</promise>\n  when green\nworking on it\n")
	rg.h.idle("sess1", protocol.WaitReview, "t1")
	rg.waitSent(1)
	require.Equal(t, protocol.LoopRunning, rg.summary(l.ID).State, "the echo is not the agent finishing")
	rg.h.work("sess1")
	rg.h.setOutput("sess1", "> Fix the build and print\n  <promise>DONE</promise>\n  when green\nall green\n<promise>DONE</promise>\n")
	rg.h.idle("sess1", protocol.WaitReview, "<promise>DONE</promise>")
	done := rg.waitState(l.ID, protocol.LoopDone)
	require.Equal(t, protocol.StopMarker, done.StopReason)
}

// max_minutes counts running time: a pause, and a long downtime, do not use it.
func TestLoop_MaxMinutesCountsRunningTimeOnly(t *testing.T) {
	rg := newRig(t)
	rg.enable()
	rg.def("m", []string{"exit", "1"}, "  max_minutes: 1\n")
	l := rg.start("m")
	rg.advance(10 * time.Second)
	_, err := rg.r.Pause(l.ID)
	require.NoError(t, err)
	rg.advance(48 * time.Hour) // paused for two days
	rg.r.Close()

	// A restart after more downtime: the loop has used 10 s of its minute.
	r2 := rg.newRunner()
	t.Cleanup(r2.Close)
	rg.advance(24 * time.Hour)
	require.NoError(t, r2.Enable())
	got, err := r2.Resume(l.ID)
	require.NoError(t, err)
	require.Equal(t, protocol.LoopRunning, got.State, "downtime and pauses are not running time")
	time.Sleep(150 * time.Millisecond)
	for _, sum := range r2.Summaries() {
		require.Equal(t, protocol.LoopRunning, sum.State)
	}
	b, err := os.ReadFile(filepath.Join(rg.dir, l.ID+".json"))
	require.NoError(t, err)
	var st State
	require.NoError(t, json.Unmarshal(b, &st))
	require.InDelta(t, float64(10*time.Second), float64(st.ElapsedNS), float64(2*time.Second))
}

// A loop that already used its minutes is capped as soon as it is resumed.
func TestLoop_ResumeAfterTheMinutesAreUsedCaps(t *testing.T) {
	rg := newRig(t)
	d := rg.def("m", []string{"exit", "1"}, "  max_minutes: 1\n")
	rg.h.sessions["sess1"] = &protocol.SessionSummary{ID: "sess1", ToolID: "claude", State: protocol.StateWorking}
	st := State{ID: "loop-0000000b", Def: d, SessionID: "sess1", State: protocol.LoopPaused, PauseReason: protocol.PauseRestart,
		Iteration: 2, ElapsedNS: int64(2 * time.Minute), StartedAt: rg.now(), UpdatedAt: rg.now()}
	require.NoError(t, writeState(rg.dir, st))
	rg.enable()
	_, err := rg.r.Resume(st.ID)
	require.NoError(t, err)
	capped := rg.waitState(st.ID, protocol.LoopCapped)
	require.Equal(t, protocol.StopMaxMinutes, capped.StopReason)
}

// The state flips to a permission prompt in the gap between the text and its
// Enter: no Enter is sent, the text stays typed, the loop pauses.
func TestLoop_PromptInTheGapBeforeEnterSendsNoEnter(t *testing.T) {
	rg := newRig(t)
	rg.enable()
	rg.def("g", []string{"exit", "1"}, "")
	l := rg.start("g")
	rg.h.gap = func() {
		rg.h.mu.Lock()
		s := rg.h.sessions["sess1"]
		s.State, s.WaitReason, s.LastLine = protocol.StateNeedsInput, protocol.WaitInput, "Allow Bash(rm -rf /)?"
		rg.h.mu.Unlock()
	}
	rg.h.idle("sess1", protocol.WaitReview, "t1")
	p := rg.waitState(l.ID, protocol.LoopPaused)
	require.Equal(t, protocol.PauseStateChanged, p.PauseReason)
	require.Zero(t, rg.h.sentCount(), "no Enter was sent")
	rg.h.mu.Lock()
	require.Equal(t, 1, rg.h.typed, "the text was typed and left unsubmitted")
	rg.h.mu.Unlock()
	require.Equal(t, 1, p.Iteration, "the turn counter did not move")
}

// The full guard also runs before the text: a state that changed with no event
// stops the send, and so does a keystroke that lands inside the send.
func TestLoop_KeystrokeInsideTheSendHoldsIt(t *testing.T) {
	rg := newRig(t)
	rg.enable()
	rg.def("k", []string{"exit", "1"}, "")
	l := rg.start("k")
	rg.h.beforeGuard = func() { rg.r.UserInput("sess1") }
	rg.h.idle("sess1", protocol.WaitReview, "t1")
	p := rg.waitState(l.ID, protocol.LoopPaused)
	require.Equal(t, protocol.PauseUserInput, p.PauseReason)
	require.Zero(t, rg.h.sentCount())
	require.Equal(t, 1, p.Iteration)
}

// No send once max_minutes is spent, even for a turn end that was queued
// ahead of the cap timer.
func TestLoop_NoSendAfterTheMinutesAreSpent(t *testing.T) {
	rg := newRig(t)
	rg.enable()
	rg.def("m", []string{"sleepexit", "200ms", "1"}, "  max_minutes: 1\n")
	l := rg.start("m")
	rg.h.idle("sess1", protocol.WaitReview, "t1") // the check starts
	rg.advance(2 * time.Minute)                   // the minutes run out while it runs
	capped := rg.waitState(l.ID, protocol.LoopCapped)
	require.Equal(t, protocol.StopMaxMinutes, capped.StopReason)
	require.Zero(t, rg.h.sentCount())
}

// Resuming an idle loop whose minutes are already spent caps it and sends
// nothing.
func TestLoop_ResumeIdleLoopOverTheLimitCapsWithoutSending(t *testing.T) {
	rg := newRig(t)
	d := rg.def("m", []string{"exit", "1"}, "  max_minutes: 1\n")
	rg.h.sessions["sess1"] = &protocol.SessionSummary{ID: "sess1", ToolID: "claude", State: protocol.StateNeedsInput,
		WaitReason: protocol.WaitReview, LastLine: "done."}
	st := State{ID: "loop-0000000c", Def: d, SessionID: "sess1", State: protocol.LoopPaused, PauseReason: protocol.PauseUser,
		Iteration: 2, ElapsedNS: int64(90 * time.Second), StartedAt: rg.now(), UpdatedAt: rg.now()}
	require.NoError(t, writeState(rg.dir, st))
	rg.enable()
	got, err := rg.r.Resume(st.ID)
	require.NoError(t, err)
	require.Equal(t, protocol.LoopCapped, got.State)
	require.Equal(t, protocol.StopMaxMinutes, got.StopReason)
	time.Sleep(100 * time.Millisecond)
	require.Zero(t, rg.h.sentCount())
}
