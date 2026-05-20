// Package protocol defines the JSONL wire format between rex and rex-daemon.
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
