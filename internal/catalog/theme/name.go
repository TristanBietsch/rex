package theme

import (
	"fmt"
	"regexp"
)

// MaxNameLen is the longest theme name.
const MaxNameLen = 32

var nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// ValidName reports whether name can name a theme: lowercase letters, digits,
// '-' and '_', starting with a letter or digit, at most MaxNameLen long. A
// theme file is <name>.yaml, so the rule also keeps names path-safe.
func ValidName(name string) error {
	if len(name) > MaxNameLen || !nameRe.MatchString(name) {
		return fmt.Errorf("theme name %q must be 1-%d characters: lowercase letters, digits, '-' or '_'", name, MaxNameLen)
	}
	return nil
}
