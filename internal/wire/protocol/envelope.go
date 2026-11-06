// Package protocol defines the JSONL wire format between rex and rex-daemon.
//
// Every line is an Envelope. Clients send intents; the daemon sends events.
// docs/protocol.md is the user-facing reference; this comment is the
// contract the daemon (internal/daemon/server) and the Go client
// (internal/wire/client) implement.
//
// # Replies
//
// An intent whose envelope carries a non-empty ID gets exactly one terminal
// reply, an event with the same ID:
//
//	Intent            Success reply                 Notes
//	Hello             Snapshot                      sent even without an ID
//	NewSession        Ack{session_id: new id}       SessionAdded is broadcast first
//	Subscribe         Ack{session_id}               after any replay/mode-sync output
//	SendInput         Ack{session_id}               bytes queued to the PTY
//	Reply             Ack{session_id}               text and Enter queued
//	Rename            Ack{session_id}               SessionUpdated is broadcast first
//	SetSessionName    Ack{session_id}               SessionUpdated is broadcast first
//	SetSessionFleet   Ack{session_id}
//	Retire, Unretire  Ack{session_id}               also when nothing changed
//	Delete            Ack{session_id}               SessionRemoved is broadcast first
//	Complete          Ack{session_id}               termination requested, not finished
//	Resize            Ack{session_id}               also for the 0x0 no-op
//	FocusFilter       Ack
//	SetMaxConcurrent  Ack
//	Reload            Ack                           after the new registry is in use
//	Shutdown          Ack                           best effort, see below
//	anything else     Error bad_intent              "intent not implemented"
//
// Any failure is an Error event (ErrorEvent) with the intent's ID instead.
// The Ack payload (Ack) names the intent type it answers.
//
// "Broadcast first" holds on a connection that has sent Hello: the
// broadcast and the reply go through the same ordered per-connection queue.
//
// Shutdown's Ack is queued before the daemon starts shutting down, but the
// daemon may close the connection before writing it; a client should treat
// the connection closing after Shutdown as success.
//
// An intent without an ID gets no Ack. It still gets an Error on failure
// (with an empty ID) and Hello still gets its Snapshot, as before acks
// existed, so older clients keep working.
//
// Protocol violations (a line over MaxLineBytes, an unsupported "v", a
// malformed line) are answered with an Error with an empty ID; for the
// first two the daemon then closes the connection.
//
// # Versions
//
// Hello.ClientVersion carries the client's release label and Hello.Client
// its program name. The daemon answers with Snapshot.DaemonVersion. A
// daemon that leaves daemon_version empty predates acks: it never sends
// Ack, so a client must not wait for one.
package protocol

import "encoding/json"

// ProtocolVersion is the wire version. Bump only on breaking changes.
const ProtocolVersion = 1

// Kind discriminates direction of a message on the wire.
type Kind string

const (
	KindIntent Kind = "Intent" // client -> daemon
	KindEvent  Kind = "Event"  // daemon -> client
)

// Envelope is the outer wrapper for every message on the wire.
type Envelope struct {
	V    int             `json:"v"`
	Kind Kind            `json:"kind"`
	Type string          `json:"type"`
	ID   string          `json:"id,omitempty"`
	Data json.RawMessage `json:"data"`
}
