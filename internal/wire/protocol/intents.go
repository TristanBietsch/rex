package protocol

// Intent types.
const (
	IntentHello            = "Hello"
	IntentSubscribe        = "Subscribe"
	IntentNewSession       = "NewSession"
	IntentSendInput        = "SendInput"
	IntentReply            = "Reply"
	IntentRename           = "Rename"
	IntentDelete           = "Delete"
	IntentResize           = "Resize"
	IntentShutdown         = "Shutdown"
	IntentSetMaxConcurrent = "SetMaxConcurrent"
	IntentSetSessionFleet  = "SetSessionFleet"
	IntentComplete         = "Complete"
	IntentSetSessionName   = "SetSessionName"
	IntentRetire           = "Retire"
	IntentUnretire         = "Unretire"
	IntentReload           = "Reload"

	// IntentFocusFilter is obsolete: board filtering is client-side. The
	// daemon still accepts it, ignoring the payload, so older clients that
	// send it get no error.
	IntentFocusFilter = "FocusFilter"
)

// Hello is the first message a client sends after connect.
type Hello struct {
	ClientVersion string `json:"client_version"`
}

// Subscribe controls what events flow back. SessionID == "" means board-wide updates only.
// Replay asks the daemon to emit the tail of the session's transcript as a SessionOutput
// event before live output starts flowing — useful for attach/scrollback.
type Subscribe struct {
	SessionID string `json:"session_id,omitempty"`
	Replay    bool   `json:"replay,omitempty"`
	// ModeSync (without Replay) asks the daemon to first send the child's
	// current terminal modes (alt screen, mouse tracking, ...) as escape
	// sequences, so the client's emulator agrees with the child's.
	ModeSync bool `json:"mode_sync,omitempty"`
}

// Resize asks the daemon to resize a running session's PTY.
type Resize struct {
	SessionID string `json:"session_id"`
	Cols      uint16 `json:"cols"`
	Rows      uint16 `json:"rows"`
}

// NewSession asks the daemon to spawn a new agent session.
type NewSession struct {
	ToolID        string `json:"tool_id"`
	ModelID       string `json:"model_id"`
	Effort        string `json:"effort,omitempty"`
	Slug          string `json:"slug"`
	Title         string `json:"title,omitempty"`
	CWD           string `json:"cwd"`
	InitialPrompt string `json:"initial_prompt,omitempty"`
	// Fleet optionally assigns the new session to a named fleet.
	Fleet string `json:"fleet,omitempty"`
}

// SetSessionFleet updates the fleet label of an existing session.
type SetSessionFleet struct {
	SessionID string `json:"session_id"`
	// Fleet is the new fleet name. Empty string clears the fleet.
	Fleet string `json:"fleet,omitempty"`
}

// SendInput forwards raw bytes to the session's PTY.
type SendInput struct {
	SessionID string `json:"session_id"`
	Bytes     []byte `json:"bytes"` // base64-encoded by encoding/json
	// Synthetic marks bytes the client sent on its own (e.g. the ^L redraw
	// nudge on attach) rather than user keystrokes. Synthetic input does not
	// cancel a pending retire.
	Synthetic bool `json:"synthetic,omitempty"`
}

// Reply is a convenience for inline text replies. The daemon drops control
// characters and escapes from Text (keeping newline and tab), types it, then
// presses Enter (\r) as a separate write ~120ms later. Use SendInput for raw
// keys.
type Reply struct {
	SessionID string `json:"session_id"`
	Text      string `json:"text"`
}

// Rename changes the slug and/or title.
type Rename struct {
	SessionID string `json:"session_id"`
	Slug      string `json:"slug,omitempty"`
	Title     string `json:"title,omitempty"`
}

// Delete removes a session.
type Delete struct {
	SessionID string `json:"session_id"`
}

// SetMaxConcurrent updates the daemon's concurrent-session cap live.
type SetMaxConcurrent struct {
	N int `json:"n"`
}

// Complete asks the daemon to cleanly terminate a running session and mark it done.
// Distinct from Delete (which removes the session entirely) and Subscribe (read-only).
type Complete struct {
	SessionID string `json:"session_id"`
}

// SetSessionName sets a session's user-facing display name. Name is sent
// without omitempty so an explicit "" clears it (falls back to the slug).
type SetSessionName struct {
	SessionID string `json:"session_id"`
	Name      string `json:"name"`
}

// Retire marks a session done: it keeps running and stays usable until
// TTLSeconds elapse, then the daemon kills it (once it is no longer working),
// removes it from the board and deletes its on-disk state. TTLSeconds == 0
// means the daemon's default. Marking an already-marked session is a no-op.
type Retire struct {
	SessionID  string `json:"session_id"`
	TTLSeconds int64  `json:"ttl_seconds,omitempty"`
}

// Unretire cancels a pending Retire. Fails with bad_state once the daemon has
// started clearing the session.
type Unretire struct {
	SessionID string `json:"session_id"`
}
