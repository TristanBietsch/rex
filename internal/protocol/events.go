package protocol

import "time"

// Event types.
const (
	EventSnapshot       = "Snapshot"
	EventSessionAdded   = "SessionAdded"
	EventSessionUpdated = "SessionUpdated"
	EventSessionRemoved = "SessionRemoved"
	EventSessionOutput  = "SessionOutput"
	EventError          = "Error"
)

// State is the lifecycle state of a session.
type State string

const (
	StateQueued     State = "queued"
	StateWorking    State = "working"
	StateNeedsInput State = "needs_input"
	StateDone       State = "done"
	StateFailed     State = "failed"
	StateCrashed    State = "crashed"
)

// SessionSummary is the canonical session shape on the wire.
type SessionSummary struct {
	ID          string    `json:"id"`
	ShortID     string    `json:"short_id"`
	ToolID      string    `json:"tool_id"`
	ModelID     string    `json:"model_id"`
	Effort      string    `json:"effort,omitempty"`
	Slug        string    `json:"slug"`
	Title       string    `json:"title,omitempty"`
	CWD         string    `json:"cwd"`
	State       State     `json:"state"`
	StartedAt   time.Time `json:"started_at"`
	LastEventAt time.Time `json:"last_event_at"`
	LastLine    string    `json:"last_line,omitempty"`
	Description string    `json:"description,omitempty"`
	ExitCode    *int      `json:"exit_code,omitempty"`

	// Token tracking (approximate: OutputBytes/4).
	Tokens      int64 `json:"tokens,omitempty"`
	OutputBytes int64 `json:"output_bytes,omitempty"`

	// Fleet groups related sessions under a named label.
	Fleet string `json:"fleet,omitempty"`
}

// Snapshot is the daemon's response to Hello: full board state.
type Snapshot struct {
	Sessions []SessionSummary `json:"sessions"`
	Filter   string           `json:"filter"`
}

// SessionUpdated carries a sparse merge-patch over the summary.
type SessionUpdated struct {
	SessionID string         `json:"session_id"`
	Patch     map[string]any `json:"patch"`
}

// SessionRemoved indicates the session is gone.
type SessionRemoved struct {
	SessionID string `json:"session_id"`
}

// SessionOutput is incremental PTY output for a subscribed session.
type SessionOutput struct {
	SessionID string `json:"session_id"`
	Bytes     []byte `json:"bytes"`
}

// ErrorEvent surfaces an error correlated to an intent's id.
type ErrorEvent struct {
	ID      string `json:"id,omitempty"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Error codes used by the daemon. Keep stable; clients may switch on them.
const (
	ErrCodeBadIntent       = "bad_intent"
	ErrCodeUnknownSession  = "unknown_session"
	ErrCodeAmbiguousID     = "ambiguous_id"
	ErrCodeRegistry        = "registry_invalid"
	ErrCodeSpawn           = "spawn_failed"
	ErrCodeTooManySessions = "too_many_sessions"
	ErrCodeBadState        = "bad_state"
)
