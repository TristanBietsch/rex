// Package state owns the in-memory session set and persistence to disk.
package state

import (
	"sync"
	"time"

	"github.com/tristanbietsch/rex/internal/protocol"
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

	// Internal — not serialized to summary.
	mu sync.Mutex
}

// Summary copies the session into a wire-friendly SessionSummary.
func (s *Session) Summary() protocol.SessionSummary {
	s.mu.Lock()
	defer s.mu.Unlock()
	return protocol.SessionSummary{
		ID:          s.ID,
		ShortID:     s.ShortID,
		ToolID:      s.ToolID,
		ModelID:     s.ModelID,
		Effort:      s.Effort,
		Slug:        s.Slug,
		Title:       s.Title,
		CWD:         s.CWD,
		State:       s.State,
		StartedAt:   s.StartedAt,
		LastEventAt: s.LastEventAt,
		LastLine:    s.LastLine,
		Description: s.Description,
		ExitCode:    s.ExitCode,
		Tokens:      s.Tokens,
		OutputBytes: s.OutputBytes,
		Fleet:       s.Fleet,
	}
}
