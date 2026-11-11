package protocol

import "time"

// Event types.
const (
	EventSnapshot         = "Snapshot"
	EventSessionAdded     = "SessionAdded"
	EventSessionUpdated   = "SessionUpdated"
	EventSessionRemoved   = "SessionRemoved"
	EventSessionOutput    = "SessionOutput"
	EventSummarizerHealth = "SummarizerHealth"
	// EventOverview carries the board-wide overview (see Overview).
	EventOverview = "Overview"
	// EventUndoResult is the success reply to Undo and Redo.
	EventUndoResult = "UndoResult"
	EventError      = "Error"
	// EventAck is the success reply to an intent that carried an ID and has
	// no more specific reply (see the package doc for the table).
	EventAck = "Ack"
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

	// Subagents is how many subagents the agent is running right now
	// (Claude Code, from its SubagentStart/SubagentStop hooks). Patches send
	// 0 when the last one stops.
	Subagents int `json:"subagents,omitempty"`

	// WaitingOn is what a working session's main thread is blocked on, one of
	// WaitShell, WaitSubagent or WaitWeb (Claude Code, from its tool hooks).
	// Empty when it is not waiting on a tool. State stays working; clients
	// that predate the field show the session as working. Patches send null
	// or "" to clear it, and it is always cleared when State leaves working.
	WaitingOn string `json:"waiting_on,omitempty"`

	// WaitingSince is when that tool call began. Zero unless WaitingOn is
	// set. Clients show a session as waiting only after WaitingAfter.
	WaitingSince time.Time `json:"waiting_since,omitzero"`

	// Fleet groups related sessions under a named label.
	Fleet string `json:"fleet,omitempty"`

	// Name is a user-chosen display label. Empty means "show the slug".
	// Unlike Slug it is free text and never used as a selector.
	Name string `json:"name,omitempty"`

	// RetireAt is when a session the user marked done gets cleared (process
	// killed, removed from the board, on-disk state deleted). Zero means not
	// marked. Orthogonal to State: a retiring session can still be working.
	RetireAt time.Time `json:"retire_at,omitzero"`

	// RetiredAt is when the user marked the session done. Zero when not
	// marked, or when the mark predates this field (older persisted state);
	// clients then show the mark without a time.
	RetiredAt time.Time `json:"retired_at,omitzero"`

	// WorkingSince is when the current uninterrupted working stretch began.
	// Zero unless State is working. Drives the long-running badge.
	WorkingSince time.Time `json:"working_since,omitzero"`

	// EndedAt is the first time the session reached a terminal state (done,
	// failed or crashed). Zero while it is live. Patches send null to clear
	// it when the session goes live again.
	EndedAt time.Time `json:"ended_at,omitzero"`

	// DescriptionAt is when Description was last written by the summarizer.
	// Zero when there is no description yet. Lets a client show how old the
	// summary is. A timestamp, not free text.
	DescriptionAt time.Time `json:"description_at,omitzero"`

	// ResumeID is the id the agent itself uses to resume this conversation
	// (a canonical UUID, lowercase; see ValidResumeID). Empty when unknown or
	// when the tool cannot resume.
	ResumeID string `json:"resume_id,omitempty"`

	// TrustFolder is whether the folder was trusted for this session when it
	// was created (NewSession.TrustFolder). Resume applies it again.
	TrustFolder bool `json:"trust_folder,omitempty"`

	// WaitReason says why a needs_input session is waiting: WaitReview (the
	// agent finished a turn) or WaitInput (it asks for an answer or an
	// approval). Empty means unknown, which is always the case for adapters
	// that only read the screen.
	WaitReason string `json:"wait_reason,omitempty"`
	// Skills are the library skills loaded into this session (NewSession.Skills).
	Skills []string `json:"skills,omitempty"`
}

// WaitReason values.
const (
	WaitReview = "review"
	WaitInput  = "input"
)

// Snapshot is the daemon's response to Hello: full board state.
type Snapshot struct {
	Sessions []SessionSummary `json:"sessions"`
	Filter   string           `json:"filter"`
	// DaemonVersion is the daemon's release label. Empty from daemons that
	// predate acks; such a daemon never sends Ack.
	DaemonVersion string `json:"daemon_version,omitempty"`
	// Overview is the daemon's current board overview. Nil until the daemon
	// has published one, and from daemons that predate it.
	Overview *Overview `json:"overview,omitempty"`
}

// Ack is the success reply to an intent that carried an envelope ID. The
// envelope's ID is the intent's.
type Ack struct {
	// Intent is the type of the intent acknowledged.
	Intent string `json:"intent"`
	// SessionID is the session the intent acted on; for NewSession, the
	// session it created. Empty for intents not about one session.
	SessionID string `json:"session_id,omitempty"`
}

// SessionUpdated carries a sparse merge-patch over the summary.
type SessionUpdated struct {
	SessionID string         `json:"session_id"`
	Patch     map[string]any `json:"patch"`
}

// SessionRemoved indicates the session is gone.
type SessionRemoved struct {
	SessionID string `json:"session_id"`
	// Reason is why it went away: RemovedByUser (Delete) or RemovedRetired
	// (a marked-done session's timer expired). Empty from older daemons.
	Reason string `json:"reason,omitempty"`
}

// SessionRemoved reasons.
const (
	RemovedByUser  = "user"
	RemovedRetired = "retired"
)

// SessionOutput is incremental PTY output for a subscribed session.
type SessionOutput struct {
	SessionID string `json:"session_id"`
	Bytes     []byte `json:"bytes"`
	// Repaint marks the SessionOutput that answers Subscribe.Repaint: Bytes
	// redraw the session's current screen instead of carrying new output.
	Repaint bool `json:"repaint,omitempty"`
}

// SummarizerHealth signals AI summary backend availability changes.
type SummarizerHealth struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

// Overview is the board-wide overview: one model-written headline over the
// buckets clients compute themselves from the sessions. It is kept in the
// daemon's memory only. Headline is untrusted model output, sanitized to at
// most MaxOverviewRunes.
type Overview struct {
	// Enabled is false while the daemon has overview_enabled off.
	Enabled     bool      `json:"enabled"`
	Headline    string    `json:"headline,omitempty"`
	GeneratedAt time.Time `json:"generated_at,omitzero"`
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
	// The daemon sends these, then closes the connection.
	ErrCodeLineTooLong        = "line_too_long"       // a line exceeded MaxLineBytes
	ErrCodeUnsupportedVersion = "unsupported_version" // envelope "v" is not ProtocolVersion
	ErrCodeInternal           = "internal_error"      // a daemon bug while handling the intent
)

// UndoResult is the reply to Undo and Redo: the files written and deleted
// (slash paths relative to the session's folder, sorted), and the tree id of
// the capture taken just before, which Redo restores after an Undo.
type UndoResult struct {
	SessionID string   `json:"session_id"`
	Restored  []string `json:"restored"`
	Deleted   []string `json:"deleted"`
	Safety    string   `json:"safety"`
}
