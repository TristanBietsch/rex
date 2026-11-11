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

	// Subagents is how many subagents the agent is running now. It is not
	// restored from meta.json: subagents do not outlive the daemon.
	Subagents int

	// WaitingOn and WaitingSince say what a working session is blocked on
	// (protocol.WaitShell and friends) and since when. Both are zero unless
	// State is working; Store.Transition, SetExit and SetWaiting keep it so.
	// They are written to meta.json but not restored by LoadAll (a restart
	// maps a working session to crashed).
	WaitingOn    string
	WaitingSince time.Time

	// Fleet optionally groups this session under a named label.
	Fleet string

	// Name is the user-chosen display label ("" = show Slug).
	Name string

	// RetireAt is when a marked-done session gets cleared (zero = not marked).
	RetireAt time.Time

	// RetiredAt is when the user marked it done (zero = not marked, or a
	// legacy mark persisted before this field existed).
	RetiredAt time.Time

	// WorkingSince is when the current working stretch began (zero unless
	// State is working). Maintained by Store.Transition.
	WorkingSince time.Time

	// EndedAt is the first time State became terminal (zero while live). It
	// is never moved later, and cleared when the session goes live again.
	EndedAt time.Time

	// ResumeID is the agent's own conversation id (valid per
	// protocol.ValidResumeID, lowercase), or "" when unknown.
	ResumeID string

	// TrustFolder is whether the folder was trusted for this session when
	// it was created (NewSession.TrustFolder). Resume applies it again.
	TrustFolder bool

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
		ID:            s.ID,
		ShortID:       s.ShortID,
		ToolID:        s.ToolID,
		ModelID:       s.ModelID,
		Effort:        s.Effort,
		Slug:          s.Slug,
		Title:         s.Title,
		CWD:           s.CWD,
		State:         s.State,
		StartedAt:     s.StartedAt,
		LastEventAt:   s.LastEventAt,
		LastLine:      s.LastLine,
		Description:   s.Description,
		DescriptionAt: s.DescriptionAt,
		ExitCode:      s.ExitCode,
		Tokens:        s.Tokens,
		OutputBytes:   s.OutputBytes,
		Subagents:     s.Subagents,
		WaitingOn:     s.WaitingOn,
		WaitingSince:  s.WaitingSince,
		Fleet:         s.Fleet,
		Name:          s.Name,
		RetireAt:      s.RetireAt,
		RetiredAt:     s.RetiredAt,
		WorkingSince:  s.WorkingSince,
		EndedAt:       s.EndedAt,
		ResumeID:      s.ResumeID,
		TrustFolder:   s.TrustFolder,
	}
}

// IsTerminal reports whether st is done, failed or crashed.
func IsTerminal(st protocol.State) bool { return isTerminal(st) }

// isTerminal reports whether st is a state a session does not leave on its own.
func isTerminal(st protocol.State) bool {
	switch st {
	case protocol.StateDone, protocol.StateFailed, protocol.StateCrashed:
		return true
	case protocol.StateQueued, protocol.StateWorking, protocol.StateNeedsInput:
		return false
	}
	return false
}

// noteStateLocked updates EndedAt for a move to newState at now and returns
// the patch entries to broadcast: ended_at (the value) when newState is
// terminal, ended_at: nil when it clears a recorded end, else nil. The first
// terminal time is kept. Caller holds s.mu.
func (s *Session) noteStateLocked(newState protocol.State, now time.Time) map[string]any {
	if isTerminal(newState) {
		if s.EndedAt.IsZero() {
			s.EndedAt = now
		}
		return map[string]any{"ended_at": s.EndedAt}
	}
	if !s.EndedAt.IsZero() {
		s.EndedAt = time.Time{}
		return map[string]any{"ended_at": nil}
	}
	return nil
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
		s.WaitingOn, s.WaitingSince = "", time.Time{}
	}
	now := time.Now().UTC()
	s.noteStateLocked(final, now)
	s.LastEventAt = now
}

func (s *Session) isRemoved() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.removed
}
