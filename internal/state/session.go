// Package state owns the in-memory session set and persistence to disk.
package state

import (
	"sync"
	"time"

	"github.com/tristanbietsch/rex/internal/protocol"
)

// Session is the daemon's view of a live or terminated session.
type Session struct {
	ID          string
	ShortID     string
	ToolID      string
	ModelID     string
	Effort      string
	Slug        string
	Title       string
	CWD         string
	State       protocol.State
	StartedAt   time.Time
	LastEventAt time.Time
	LastLine    string
	ExitCode    *int

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
		ExitCode:    s.ExitCode,
	}
}
