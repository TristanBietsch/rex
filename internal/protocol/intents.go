package protocol

// Intent types.
const (
	IntentHello       = "Hello"
	IntentSubscribe   = "Subscribe"
	IntentNewSession  = "NewSession"
	IntentOpenSession = "OpenSession"
	IntentSendInput   = "SendInput"
	IntentReply       = "Reply"
	IntentRename      = "Rename"
	IntentDelete      = "Delete"
	IntentResize      = "Resize"
	IntentFocusFilter      = "FocusFilter"
	IntentShutdown         = "Shutdown"
	IntentSetMaxConcurrent = "SetMaxConcurrent"
	IntentSetSessionFleet  = "SetSessionFleet"
	IntentComplete         = "Complete"
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

// OpenSession marks a session as the client's foreground.
type OpenSession struct {
	SessionID string `json:"session_id"`
}

// SendInput forwards raw bytes to the session's PTY.
type SendInput struct {
	SessionID string `json:"session_id"`
	Bytes     []byte `json:"bytes"` // base64-encoded by encoding/json
}

// Reply is a convenience for inline text replies. Daemon appends a newline.
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

// FocusFilter is a cosmetic intent stored per-client.
type FocusFilter struct {
	ToolID string `json:"tool_id"` // "all" or a tool id
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
