package state

import "github.com/tristanbietsch/rex/internal/wire/protocol"

// Requester says which process asked the daemon for something: the client
// name it sent in Hello, its pid and its executable path. All three are
// chosen or reported by the peer (the path is kernel-verified on Linux
// only), so they identify, they do not prove. Every string is flattened
// display text.
type Requester struct {
	Client string `json:"client,omitempty"`
	PID    int    `json:"pid,omitempty"`
	Exe    string `json:"exe,omitempty"`
}

// clean returns r with its strings flattened (meta.json is not trusted),
// or nil when nothing is left.
func (r *Requester) clean() *Requester {
	if r == nil {
		return nil
	}
	c := Requester{
		Client: protocol.FlattenDisplayText(r.Client),
		PID:    max(r.PID, 0),
		Exe:    protocol.FlattenDisplayText(r.Exe),
	}
	if c == (Requester{}) {
		return nil
	}
	return &c
}

// SetRequestedBy records who started this run of the session (nil clears
// it). It is persisted in meta.json and copied into the run's audit record.
func (s *Session) SetRequestedBy(r *Requester) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.RequestedBy = r.clean()
}

// Requester returns a copy of who started this run of the session, nil when
// unknown.
func (s *Session) Requester() *Requester {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.RequestedBy == nil {
		return nil
	}
	c := *s.RequestedBy
	return &c
}
