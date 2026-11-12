package loop

import (
	"context"
	"errors"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// Event is the part of a session store event the runner reads.
type Event struct {
	SessionID string
	// State is the session's new state; "" when the event does not change it.
	State protocol.State
	// WaitReason is the session's new wait reason when the event patches it
	// (possibly ""); nil otherwise.
	WaitReason *string
	// Removed is true when the session left the board.
	Removed bool
}

// SpawnRequest asks the host to start a loop's session through the normal
// spawn path.
type SpawnRequest struct {
	LoopID string
	// Iter is the loop turn the session starts (always 1).
	Iter int
	// Slug and Title name the session.
	Slug, Title string
	Tool, Model string
	Effort      string
	CWD         string
	// Prompt is the session's initial prompt.
	Prompt string
}

// Host is what the runner needs from the daemon. The daemon implements it
// over its server and session store (internal/daemon/boot); tests use a fake.
type Host interface {
	// Spawn starts the loop's session and returns its id. Errors are the
	// spawn path's own (bad tool, too many sessions, ...).
	Spawn(ctx context.Context, req SpawnRequest) (sessionID string, err error)
	// Send types text into the session and presses Enter, as a Reply does.
	Send(sessionID, text string) error
	// Session is the session's current summary.
	Session(sessionID string) (protocol.SessionSummary, bool)
	// Output is the session's latest readable output (its screen).
	Output(sessionID string) string
	// ReportsReason says whether the tool reports why it waits (Claude Code
	// hooks): for those, a turn end is an explicit wait_reason "review".
	ReportsReason(toolID string) bool
	// EnsureLive makes sure the session runs again (resuming an exited one
	// in place). It returns nil at once for a live session.
	EnsureLive(ctx context.Context, sessionID string) error
	// SetSessionLoop records the loop and its turn on the session, which
	// shows on the board and the wire.
	SetSessionLoop(sessionID, loopID string, iter int)
	// Subscribe delivers session events until the returned cancel is called.
	// fn must not block: it is called on the store's goroutines.
	Subscribe(fn func(Event)) (cancel func())
	// Publish broadcasts a loop's new state to connected clients.
	Publish(protocol.LoopSummary)
}

// Kind classifies an Error for the wire.
type Kind int

const (
	// KindBadInput: the request or the loop file is wrong, or the feature is
	// off, or the loop cap is reached (bad_intent).
	KindBadInput Kind = iota
	// KindNotFound: no such loop (bad_intent).
	KindNotFound
	// KindBadState: the loop is not in a state that allows this (bad_state).
	KindBadState
	// KindSpawn: the session could not start; Err is the spawn path's error.
	KindSpawn
)

// Error is a refusal the runner reports to the client.
type Error struct {
	Kind Kind
	Msg  string
	// Err is the underlying error, for KindSpawn.
	Err error
}

func (e *Error) Error() string { return e.Msg }
func (e *Error) Unwrap() error { return e.Err }

// Msgs the clients and tests match on.
const (
	MsgOff     = "off: enable loops in Settings → Features"
	MsgTooMany = "too many loops"
)

// ErrOff is returned while the loops feature is off.
var ErrOff = &Error{Kind: KindBadInput, Msg: MsgOff}

func badInput(msg string) *Error { return &Error{Kind: KindBadInput, Msg: msg} }

var errNotFound = &Error{Kind: KindNotFound, Msg: "unknown loop"}

// IsOff reports whether err is the feature-off refusal.
func IsOff(err error) bool { return errors.Is(err, ErrOff) }
