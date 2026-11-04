package server

import (
	"errors"
	"fmt"
	"strings"

	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// requireSessionID rejects a missing session_id and one that could not name
// a session directory (e.g. "../.."), before any handler builds a path or
// looks it up.
func requireSessionID(id string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("session_id required")
	}
	if err := state.ValidateSessionID(id); err != nil {
		return fmt.Errorf("invalid session_id: %w", err)
	}
	return nil
}

func validateNewSession(p protocol.NewSession) error {
	if strings.TrimSpace(p.ToolID) == "" {
		return fmt.Errorf("tool_id required")
	}
	if strings.TrimSpace(p.ModelID) == "" {
		return fmt.Errorf("model_id required")
	}
	if err := protocol.ValidateSlug(p.Slug); err != nil {
		return err
	}
	if strings.TrimSpace(p.CWD) == "" {
		return fmt.Errorf("cwd required")
	}
	return nil
}

// badIntentError marks a handler error as the client's fault, so the
// connection loop reports it as bad_intent instead of a handler-specific code.
type badIntentError struct{ err error }

func (e *badIntentError) Error() string { return e.err.Error() }
func (e *badIntentError) Unwrap() error { return e.err }

func badIntent(err error) error { return &badIntentError{err: err} }

func isBadIntent(err error) bool {
	var bi *badIntentError
	return errors.As(err, &bi)
}

// validateRename checks a Rename before it reaches the store: at least one of
// slug/title, and a slug (if given) in the same format NewSession requires.
func validateRename(p protocol.Rename) error {
	if p.Slug == "" && p.Title == "" {
		return fmt.Errorf("slug or title required")
	}
	if p.Slug != "" {
		if err := protocol.ValidateSlug(p.Slug); err != nil {
			return err
		}
	}
	return nil
}
