// Package state owns the in-memory session set and persistence to disk.
package state

import (
	"sync"
	"time"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// Session is the daemon's view of a live or terminated session.
type Session struct {
	ID            string
	ShortID       string
	ToolID        string
	ModelID       string
	Effort        string
	Slug          string
	Title         string
	CWD           string
	State         protocol.State
	StartedAt     time.Time
	LastEventAt   time.Time
	LastLine      string
	Description   string
	DescriptionAt time.Time
	ExitCode      *int

	// OutputBytes is the raw byte count of PTY output for this session.
	// Tokens is an approximate token count derived as OutputBytes / 4.
	// This is a rough heuristic and should not be treated as exact.
	OutputBytes int64
	Tokens      int64

	// Fleet optionally groups this session under a named label.
	Fleet string

	// Name is the user-chosen display label ("" = show Slug).
	Name string

	// RetireAt is when a marked-done session gets cleared (zero = not marked).
	RetireAt time.Time

	// WorkingSince is when the current working stretch began (zero unless
	// State is working). Maintained by Store.Transition.
	WorkingSince time.Time

	// Internal — not serialized to summary.
	mu sync.Mutex
	// reaping is set once the retire sweep has claimed the session for
	// deletion; Unretire is refused from then on.
	reaping bool
	// removed is set when the session leaves the store; WriteMeta skips
	// removed sessions so a late write can't resurrect a deleted session.
	removed bool
	// metaMu serializes meta.json writes and the directory removal so writers
	// can't interleave temp files or recreate the dir after delete.
	metaMu sync.Mutex
	// screen is the readable text of the agent's current virtual terminal
	// screen (chrome filtered). Feeds the summarizer; never persisted.
	screen string
}

// Summary copies the session into a wire-friendly SessionSummary.
func (s *Session) Summary() protocol.SessionSummary {
	s.mu.Lock()
	defer s.mu.Unlock()
	return protocol.SessionSummary{
		ID:           s.ID,
		ShortID:      s.ShortID,
		ToolID:       s.ToolID,
		ModelID:      s.ModelID,
		Effort:       s.Effort,
		Slug:         s.Slug,
		Title:        s.Title,
		CWD:          s.CWD,
		State:        s.State,
		StartedAt:    s.StartedAt,
		LastEventAt:  s.LastEventAt,
		LastLine:     s.LastLine,
		Description:  s.Description,
		ExitCode:     s.ExitCode,
		Tokens:       s.Tokens,
		OutputBytes:  s.OutputBytes,
		Fleet:        s.Fleet,
		Name:         s.Name,
		RetireAt:     s.RetireAt,
		WorkingSince: s.WorkingSince,
	}
}

// SetExit records how the child exited (state + optional exit code) under
// the session lock. The supervisor calls it before persisting final meta.
// The exit is real activity, so it bumps LastEventAt (the session's end).
func (s *Session) SetExit(final protocol.State, code *int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.State = final
	if code != nil {
		c := *code
		s.ExitCode = &c
	}
	if final != protocol.StateWorking {
		s.WorkingSince = time.Time{}
	}
	s.LastEventAt = time.Now().UTC()
}

func (s *Session) isRemoved() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.removed
}
