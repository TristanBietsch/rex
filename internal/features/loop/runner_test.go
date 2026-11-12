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
}

func newFakeHost() *fakeHost {
	return &fakeHost{
		sessions: map[string]*protocol.SessionSummary{}, output: map[string]string{},
		loopIter: map[string]int{}, claude: true,
	}
}

func (h *fakeHost) Spawn(_ context.Context, req SpawnRequest) (string, error) {
	h.mu.Lock()
	if h.spawnErr != nil {
		h.mu.Unlock()
		return "", h.spawnErr
	}
	h.nextID++
	id := "sess" + strconv.Itoa(h.nextID)
	h.sessions[id] = &protocol.SessionSummary{ID: id, ToolID: req.Tool, State: protocol.StateWorking}
	h.spawns = append(h.spawns, req)
	subs := append([]func(Event){}, h.subs...)
	h.mu.Unlock()
	// The session starts working before Spawn returns, like a real one.
	for _, fn := range subs {
		fn(Event{SessionID: id, State: protocol.StateWorking})
	}
	return id, nil
}

func (h *fakeHost) Send(id, text string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.sendErr != nil {
		return h.sendErr
	}
	h.sent = append(h.sent, id+":"+text)
	return nil
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

func (h *fakeHost) EnsureLive(_ context.Context, id string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
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
	t      *testing.T
	h      *fakeHost
	r      *Runner
	dir    string
	defs   map[string]Def
	loaded []string
}

func newRig(t *testing.T) *rig {
	t.Helper()
	t.Setenv("REX_LOOP_HELPER", "1")
	rg := &rig{t: t, h: newFakeHost(), dir: filepath.Join(t.TempDir(), "loops"), defs: map[string]Def{}}
	rg.r = rg.newRunner()
	t.Cleanup(func() { rg.r.Close() })
	return rg
}

func (rg *rig) newRunner() *Runner {
	return NewRunner(Config{
		Host: rg.h, StateDir: rg.dir, MinuteUnit: time.Minute,
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
	for _, claude := range []bool{true, false} {
		t.Run("claude="+strconv.FormatBool(claude), func(t *testing.T) {
			rg := newRig(t)
			rg.h.claude = claude
			rg.enable()
			rg.def("q", []string{"exit", "1"}, "")
			l := rg.start("q")
			rg.h.work("sess1")
			rg.h.idle("sess1", map[bool]string{true: protocol.WaitReview, false: ""}[claude], "Should I delete the old tests?")
			p := rg.waitState(l.ID, protocol.LoopPaused)
			require.Equal(t, protocol.PauseNeedsInput, p.PauseReason)
			time.Sleep(150 * time.Millisecond)
			require.Zero(t, rg.h.sentCount())
		})
	}
}

// Tools that give no wait reason: working -> needs_input is a turn end.
func TestLoop_NonClaudeTurnEnd(t *testing.T) {
	rg := newRig(t)
	rg.h.claude = false
	rg.enable()
	rg.def("plain", []string{"exit", "1"}, "")
	rg.start("plain")
	rg.h.idle("sess1", "", "finished a step")
	rg.waitSent(1)
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

	// Resume brings the session back, treats it as idle and carries on.
	s, err := r2.Resume(l.ID)
	require.NoError(t, err)
	require.Equal(t, protocol.LoopRunning, s.State)
	require.Equal(t, []string{"sess1"}, rg.h.live)
	require.Eventually(t, func() bool { return rg.h.sentCount() == 2 }, 15*time.Second, 10*time.Millisecond)
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
