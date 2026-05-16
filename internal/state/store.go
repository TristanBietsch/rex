package state

import (
	"fmt"
	"sync"
	"time"

	"github.com/tristanbietsch/rex/internal/protocol"
)

// EventKind classifies a store event.
type EventKind int

const (
	EventAdded EventKind = iota
	EventUpdated
	EventRemoved
)

// Event is what subscribers receive on every change.
type Event struct {
	Kind      EventKind
	SessionID string
	NewState  *protocol.State          // set on Updated when state changed
	Patch     map[string]any           // set on Updated for arbitrary field changes
	Summary   *protocol.SessionSummary // set on Added
}

// Store is the central session table.
type Store struct {
	mu          sync.RWMutex
	sessions    map[string]*Session
	byShortID   map[string]string // short_id -> id
	subscribers []func(Event)
	subsMu      sync.RWMutex
}

// NewStore returns an empty store.
func NewStore() *Store {
	return &Store{
		sessions:  make(map[string]*Session),
		byShortID: make(map[string]string),
	}
}

// Add inserts a session. Errors if the ID is taken.
func (s *Store) Add(sess *Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.sessions[sess.ID]; exists {
		return fmt.Errorf("session %s already exists", sess.ID)
	}
	s.sessions[sess.ID] = sess
	s.byShortID[sess.ShortID] = sess.ID

	sum := sess.Summary()
	s.broadcast(Event{Kind: EventAdded, SessionID: sess.ID, Summary: &sum})
	return nil
}

// Get returns a session by full id.
func (s *Store) Get(id string) (*Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.sessions[id]
	return sess, ok
}

// GetByShortID returns a session by its 4-char short id (or extended).
func (s *Store) GetByShortID(short string) (*Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	id, ok := s.byShortID[short]
	if !ok {
		return nil, false
	}
	return s.sessions[id], true
}

// All returns a snapshot of every session (pointer values; do not mutate without locking).
func (s *Store) All() []*Session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		out = append(out, sess)
	}
	return out
}

// Snapshot returns wire-shaped summaries for every session.
func (s *Store) Snapshot() []protocol.SessionSummary {
	all := s.All()
	out := make([]protocol.SessionSummary, len(all))
	for i, sess := range all {
		out[i] = sess.Summary()
	}
	return out
}

// Remove deletes a session and emits.
func (s *Store) Remove(id string) error {
	s.mu.Lock()
	sess, ok := s.sessions[id]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("session %s not found", id)
	}
	delete(s.sessions, id)
	delete(s.byShortID, sess.ShortID)
	s.mu.Unlock()

	s.broadcast(Event{Kind: EventRemoved, SessionID: id})
	return nil
}

// Transition changes a session's state and emits.
func (s *Store) Transition(id string, newState protocol.State) error {
	s.mu.RLock()
	sess, ok := s.sessions[id]
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("session %s not found", id)
	}
	sess.mu.Lock()
	sess.State = newState
	sess.LastEventAt = time.Now().UTC()
	sess.mu.Unlock()

	ns := newState
	s.broadcast(Event{
		Kind:      EventUpdated,
		SessionID: id,
		NewState:  &ns,
		Patch:     map[string]any{"state": newState, "last_event_at": time.Now().UTC()},
	})
	return nil
}

// UpdateTokens increments the session's OutputBytes by deltaBytes and recomputes
// Tokens = OutputBytes / 4 (approximate heuristic).
// Returns the new token count, the new raw output byte count, and any error.
// Callers should emit a SessionUpdated patch when tokens cross a threshold.
func (s *Store) UpdateTokens(id string, deltaBytes int64) (tokens, outputBytes int64, err error) {
	s.mu.RLock()
	sess, ok := s.sessions[id]
	s.mu.RUnlock()
	if !ok {
		return 0, 0, fmt.Errorf("session %s not found", id)
	}
	sess.mu.Lock()
	sess.OutputBytes += deltaBytes
	sess.Tokens = sess.OutputBytes / 4
	tokens = sess.Tokens
	outputBytes = sess.OutputBytes
	sess.mu.Unlock()
	return tokens, outputBytes, nil
}

// BroadcastTokenPatch emits a SessionUpdated event for the token/output_bytes fields.
func (s *Store) BroadcastTokenPatch(id string, tokens, outputBytes int64) {
	s.broadcast(Event{
		Kind:      EventUpdated,
		SessionID: id,
		Patch:     map[string]any{"tokens": tokens, "output_bytes": outputBytes},
	})
}

// SetFleet updates the fleet label on a session, persists it, and broadcasts.
func (s *Store) SetFleet(id, fleet string) error {
	s.mu.RLock()
	sess, ok := s.sessions[id]
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("session %s not found", id)
	}
	sess.mu.Lock()
	sess.Fleet = fleet
	sess.LastEventAt = time.Now().UTC()
	sess.mu.Unlock()

	s.broadcast(Event{
		Kind:      EventUpdated,
		SessionID: id,
		Patch:     map[string]any{"fleet": fleet, "last_event_at": time.Now().UTC()},
	})
	return nil
}

// UpdateLastLine records a transcript-derived summary line.
func (s *Store) UpdateLastLine(id, line string) error {
	s.mu.RLock()
	sess, ok := s.sessions[id]
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("session %s not found", id)
	}
	sess.mu.Lock()
	sess.LastLine = line
	sess.LastEventAt = time.Now().UTC()
	sess.mu.Unlock()

	s.broadcast(Event{
		Kind:      EventUpdated,
		SessionID: id,
		Patch:     map[string]any{"last_line": line, "last_event_at": time.Now().UTC()},
	})
	return nil
}

// Subscribe registers a callback for store events. Returns a cancel func.
func (s *Store) Subscribe(fn func(Event)) func() {
	s.subsMu.Lock()
	defer s.subsMu.Unlock()
	s.subscribers = append(s.subscribers, fn)
	idx := len(s.subscribers) - 1
	return func() {
		s.subsMu.Lock()
		defer s.subsMu.Unlock()
		if idx < len(s.subscribers) {
			s.subscribers[idx] = nil
		}
	}
}

func (s *Store) broadcast(e Event) {
	s.subsMu.RLock()
	defer s.subsMu.RUnlock()
	for _, fn := range s.subscribers {
		if fn != nil {
			fn(e)
		}
	}
}
