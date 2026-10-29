package server

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func requireSessionID(id string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("session_id required")
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

// Caps on free text from clients, in runes. A title is display text (the
// CLI sends the whole prompt as one), so a long one is truncated; a fleet is
// a label the user typed, so a long one is rejected.
const (
	maxTitleRunes = 512
	maxFleetRunes = 64
)

// normalizeTitle makes a session title safe for one board row and caps it.
// The agent never sees it: the prompt travels separately (InitialPrompt).
func normalizeTitle(s string) string {
	return truncateRunes(protocol.SingleLine(s), maxTitleRunes)
}

// normalizeFleet makes a fleet label safe for one row. "" clears it.
func normalizeFleet(s string) (string, error) {
	out := protocol.SingleLine(s)
	if utf8.RuneCountInString(out) > maxFleetRunes {
		return "", fmt.Errorf("fleet exceeds %d characters", maxFleetRunes)
	}
	return out, nil
}

func truncateRunes(s string, n int) string {
	count := 0
	for i := range s {
		if count == n {
			return s[:i]
		}
		count++
	}
	return s
}

// validateCWD checks that the spawn directory exists, so a bad path fails
// the NewSession instead of surfacing later as a failed session.
func validateCWD(dir string) error {
	fi, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("cwd: %w", err)
	}
	if !fi.IsDir() {
		return fmt.Errorf("cwd %s: not a directory", dir)
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
