package protocol

import (
	"errors"
	"fmt"
	"unicode"
	"unicode/utf8"
)

// MaxSlugRunes caps a session slug. Derived slugs stay well under it.
const MaxSlugRunes = 64

// ValidateSlug checks a session slug: non-empty, at most MaxSlugRunes, valid
// UTF-8, and free of whitespace and control characters, so it stays one
// token for selectors (`rex attach <slug>`, TUI `:rm <slug>`) and cannot
// inject terminal escapes. Uniqueness is enforced separately by the
// daemon's store.
func ValidateSlug(s string) error {
	if s == "" {
		return errors.New("slug required")
	}
	if !utf8.ValidString(s) {
		return errors.New("slug is not valid UTF-8")
	}
	if utf8.RuneCountInString(s) > MaxSlugRunes {
		return fmt.Errorf("slug exceeds %d characters", MaxSlugRunes)
	}
	for _, r := range s {
		if unicode.IsSpace(r) {
			return errors.New("slug must not contain whitespace")
		}
		if unicode.IsControl(r) || isBidiControl(r) {
			return errors.New("slug must not contain control characters")
		}
	}
	return nil
}
