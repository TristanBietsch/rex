package protocol

import (
	"regexp"
	"time"
)

// Loop intents (repeat loop workflows, the loops feature). Each carries an ID
// to be answered: the reply is a Loop event with the envelope's ID holding
// the loop's new state. While the feature is off they fail with bad_intent
// ("off: ..."); a daemon that predates them answers bad_intent "intent not
// implemented".
const (
	IntentStartLoop  = "StartLoop"
	IntentPauseLoop  = "PauseLoop"
	IntentResumeLoop = "ResumeLoop"
	IntentStopLoop   = "StopLoop"
)

// EventLoop carries a LoopSummary: broadcast on every change of a loop, and
// the reply to the four loop intents.
const EventLoop = "Loop"

// StartLoop starts the loop defined by ~/.config/rex/loops/<Name>.yaml. The
// daemon reads that file itself; a client never sends a path or a command.
type StartLoop struct {
	// Name is the loop file's name without ".yaml".
	Name string `json:"name"`
	// CWD overrides the file's cwd. An absolute folder without "..". One of
	// the two must be set.
	CWD string `json:"cwd,omitempty"`
}

// PauseLoop stops re-sending prompts until ResumeLoop. The session keeps
// running.
type PauseLoop struct {
	LoopID string `json:"loop_id"`
}

// ResumeLoop continues a paused loop. A loop whose session exited (after a
// daemon restart) gets its session resumed first.
type ResumeLoop struct {
	LoopID string `json:"loop_id"`
}

// StopLoop ends a loop for good (state stopped). The session keeps running.
type StopLoop struct {
	LoopID string `json:"loop_id"`
}

// LoopState is a loop's lifecycle state.
type LoopState string

const (
	LoopRunning LoopState = "running"
	LoopPaused  LoopState = "paused"
	// LoopDone: the check passed or the marker showed up.
	LoopDone LoopState = "done"
	// LoopCapped: max_iterations or max_minutes was reached.
	LoopCapped LoopState = "capped"
	// LoopStopped: the user stopped it.
	LoopStopped LoopState = "stopped"
	// LoopFailed: the loop could not go on (the check could not start).
	LoopFailed LoopState = "failed"
)

// Terminal reports whether the loop has ended for good.
func (s LoopState) Terminal() bool {
	switch s {
	case LoopDone, LoopCapped, LoopStopped, LoopFailed:
		return true
	}
	return false
}

// LoopSummary.PauseReason values.
const (
	// PauseUser: PauseLoop. Only ResumeLoop continues.
	PauseUser = "user"
	// PauseNeedsInput: the session asks a question or for an approval. The
	// loop never answers; it continues by itself once the agent finishes a
	// turn after you answered.
	PauseNeedsInput = "needs_input"
	// PauseUserInput: you typed into the session. Re-sends wait until the
	// agent has finished a turn after that.
	PauseUserInput = "user_input"
	// PauseRestart: the daemon restarted. Only ResumeLoop continues.
	PauseRestart = "daemon_restart"
	// PauseSessionEnded: the session exited. ResumeLoop resumes it.
	PauseSessionEnded = "session_ended"
	// PauseSendFailed: the prompt could not be delivered.
	PauseSendFailed = "send_failed"
	// PauseOff: the loops feature was switched off.
	PauseOff = "feature_off"
)

// LoopSummary.StopReason values.
const (
	StopCheckPassed   = "check_passed"
	StopMarker        = "marker"
	StopMaxIterations = "max_iterations"
	StopMaxMinutes    = "max_minutes"
	StopByUser        = "stopped"
	StopCheckError    = "check_error"
)

// LoopSummary is a loop on the wire.
type LoopSummary struct {
	ID    string    `json:"id"`
	Name  string    `json:"name"`
	Kind  string    `json:"kind"`
	State LoopState `json:"state"`
	// PauseReason says why a paused loop waits (Pause*); empty otherwise.
	PauseReason string `json:"pause_reason,omitempty"`
	// StopReason says why a finished loop ended (Stop*); empty while live.
	StopReason string `json:"stop_reason,omitempty"`
	// SessionID is the loop's session.
	SessionID string `json:"session_id,omitempty"`
	CWD       string `json:"cwd,omitempty"`
	// Iteration is the turn in progress (1 for the first prompt);
	// MaxIterations is the cap.
	Iteration     int `json:"iteration"`
	MaxIterations int `json:"max_iterations"`
	// LastCheckExit is the exit status of the latest check run; nil before
	// the first run or when it did not exit (timeout, start failure).
	LastCheckExit *int      `json:"last_check_exit,omitempty"`
	StartedAt     time.Time `json:"started_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	// Notice is a line to show the user: a cap was reached, a check could
	// not start, another loop shares the folder. Daemon-written text.
	Notice string `json:"notice,omitempty"`
}

var loopIDRE = regexp.MustCompile(`^loop-[0-9a-f]{8}$`)

// ValidLoopID reports whether id has the form the daemon gives loops
// ("loop-" and 8 hex digits). Loop ids end up in file names, so every id a
// client sends is checked.
func ValidLoopID(id string) bool { return loopIDRE.MatchString(id) }
