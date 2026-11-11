package protocol

// Intent types.
const (
	IntentHello            = "Hello"
	IntentSubscribe        = "Subscribe"
	IntentNewSession       = "NewSession"
	IntentOpenSession      = "OpenSession"
	IntentSendInput        = "SendInput"
	IntentReply            = "Reply"
	IntentRename           = "Rename"
	IntentDelete           = "Delete"
	IntentResize           = "Resize"
	IntentFocusFilter      = "FocusFilter"
	IntentShutdown         = "Shutdown"
	IntentSetMaxConcurrent = "SetMaxConcurrent"
	IntentSetSessionFleet  = "SetSessionFleet"
	IntentComplete         = "Complete"
	IntentSetSessionName   = "SetSessionName"
	IntentRetire           = "Retire"
	IntentUnretire         = "Unretire"
	IntentResume           = "Resume"
	IntentReload           = "Reload"
	IntentUndo             = "Undo"
	IntentRedo             = "Redo"
)

// Hello is the first message a client sends after connect. The daemon
// answers with a Snapshot and from then on broadcasts board events to the
// connection.
type Hello struct {
	// ClientVersion is the client's release label (package version).
	// Clients before acks sent their program name here.
	ClientVersion string `json:"client_version"`
	// Client names the client program ("rex-cli", "rex-tui", ...), for
	// the daemon log and, for ClientTUI, to count a board viewer. Empty from
	// older clients.
	Client string `json:"client,omitempty"`
}

// ClientTUI is the Hello.Client of the rex board. The daemon counts only
// these connections as someone looking at the board.
const ClientTUI = "rex-tui"

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
	// Repaint asks the daemon to send, after any replay and before live
	// output, one SessionOutput with Repaint set whose bytes redraw the
	// session's current screen (modes, cells, cursor) from the daemon's
	// screen mirror. It covers what ModeSync would send. Nothing is sent for
	// a session that is not running. A daemon that predates the field
	// ignores it; a client tells by seeing no repaint before the Subscribe's
	// reply.
	Repaint bool `json:"repaint,omitempty"`
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
	// TrustFolder asks the daemon to trust CWD for this session as if it
	// were listed in auto_trust_dirs: the agent's folder-trust dialog is
	// answered or avoided, and skip_permissions may apply. CWD must be an
	// absolute folder that is neither a filesystem root nor the home folder
	// and has no "..", else the intent is bad_intent. The choice is kept for
	// Resume.
	TrustFolder bool `json:"trust_folder,omitempty"`
	// Name is the session's display name (see NormalizeSessionName); empty
	// shows the slug. SetSessionName changes it later.
	Name string `json:"name,omitempty"`
	// Skills are names from the daemon's skill library to load into this
	// session only (at most 16, unique). The daemon copies them into the
	// session directory; the tool must declare skill_args and the
	// spawn_skills setting must be on, else the intent is bad_intent. Names
	// only: the daemon never takes a path from a client.
	Skills []string `json:"skills,omitempty"`
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
	// Synthetic marks bytes the client sent on its own (e.g. the ^L redraw
	// nudge on attach) rather than user keystrokes. Synthetic input does not
	// cancel a pending retire.
	Synthetic bool `json:"synthetic,omitempty"`
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

// Resume makes a session live and not marked done. A running session just
// loses its done mark. An exited (done, failed or crashed) session is
// respawned in place with the tool's resume arguments: same id, slug, row
// and transcript. Fails with bad_state when the tool cannot resume the
// session (no resume block, or no known resume id) or the daemon has
// started clearing it, and with too_many_sessions at the concurrency cap.
type Resume struct {
	SessionID string `json:"session_id"`
}

// Undo restores a session's folder to how it was before its last Turns
// agent turns (default 1), from the turn snapshots (setting turn_snapshots).
// The daemon first captures the folder's current state, so Redo can bring it
// back. The reply is an UndoResult. It fails with bad_intent when turn
// snapshots are off, and with bad_state when the session is working, when
// another live session shares the folder (unless Force), when the folder is
// not inside a repository, or when there is no recorded turn to undo.
type Undo struct {
	SessionID string `json:"session_id"`
	// Turns is how many turns to go back. 0 means 1.
	Turns int `json:"turns,omitempty"`
	// Force undoes although another live session works in the same folder
	// (its changes are reverted too).
	Force bool `json:"force,omitempty"`
}

// Redo brings back the state the latest Undo replaced. The reply is an
// UndoResult. Redo history is dropped when the session starts another turn.
// The failures are Undo's, plus bad_state when nothing is waiting to be
// redone.
type Redo struct {
	SessionID string `json:"session_id"`
	// Force: as for Undo.
	Force bool `json:"force,omitempty"`
}
