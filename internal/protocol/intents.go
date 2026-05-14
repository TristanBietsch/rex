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
	IntentFocusFilter = "FocusFilter"
	IntentShutdown    = "Shutdown"
)

// Hello is the first message a client sends after connect.
type Hello struct {
	ClientVersion string `json:"client_version"`
}

// Subscribe controls what events flow back. SessionID == "" means board-wide updates only.
type Subscribe struct {
	SessionID string `json:"session_id,omitempty"`
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
