package server

import (
	"errors"
	"fmt"
	"path/filepath"
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
	// The daemon's own working directory is meaningless to the client.
	if !filepath.IsAbs(p.CWD) {
		return fmt.Errorf("cwd must be an absolute path, got %q", p.CWD)
	}
	if p.TrustFolder {
		if _, err := sessionTrustDir(p.CWD); err != nil {
			return fmt.Errorf("trust_folder: cannot trust cwd: %w", err)
		}
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

// Free-text fields (title, fleet) have no format to validate, so they are
// sanitized on ingest (S-03): escape sequences, control characters and
// everything after the first line go, and the length is capped. Slugs and
// names keep their existing validation, which rejects instead. A value that
// was non-empty but has no displayable text left is rejected rather than
// silently clearing the field.

// sanitizeField returns raw as one line of display-safe text, or an error
// naming field when raw was non-empty and nothing displayable is left.
func sanitizeField(field, raw string, maxRunes int) (string, error) {
	clean := protocol.SanitizeDisplayText(raw, maxRunes)
	if clean == "" && raw != "" {
		return "", fmt.Errorf("%s has no displayable text (control characters and escape sequences are removed)", field)
	}
	return clean, nil
}

// normalizeNewSession sanitizes a NewSession's title and fleet. The title
// is the task prompt for most clients, so only its first line survives; the
// prompt itself travels separately in InitialPrompt.
func normalizeNewSession(p protocol.NewSession) (protocol.NewSession, error) {
	// A title of only control bytes is dropped, not rejected: it is optional
	// and usually derived from the prompt rather than typed as a label.
	p.Title = protocol.SanitizeDisplayText(p.Title, protocol.MaxTitleRunes)
	fleet, err := sanitizeField("fleet", p.Fleet, protocol.MaxFleetRunes)
	if err != nil {
		return p, err
	}
	p.Fleet = fleet
	return p, nil
}

// normalizeRename sanitizes a Rename's title (the slug is validated, not
// sanitized, by validateRename).
func normalizeRename(p protocol.Rename) (protocol.Rename, error) {
	title, err := sanitizeField("title", p.Title, protocol.MaxTitleRunes)
	if err != nil {
		return p, err
	}
	p.Title = title
	return p, nil
}
