package settings

import (
	"fmt"
	"strings"

	"github.com/tristanbietsch/rex/internal/catalog/theme"
)

// maxAccentLen bounds a typed accent_color value.
const maxAccentLen = 64

// normalizeAccentColor is the Normalize func of accent_color: "theme", a
// named accent color, or a color in one of the forms theme.ParseAccent
// accepts. The value is kept as typed (trimmed and lowercased), so the
// settings row shows what was chosen; it is resolved to a color when the
// theme is applied.
func normalizeAccentColor(v string) (string, error) {
	v = strings.ToLower(strings.TrimSpace(v))
	if len(v) > maxAccentLen {
		return "", fmt.Errorf("accent color is too long (%d characters at most)", maxAccentLen)
	}
	c, err := theme.ParseAccent(v)
	if err != nil {
		return "", err
	}
	switch {
	case c.Dark == "":
		return theme.AccentDefault, nil
	case c.Ref != "custom":
		return c.ID, nil // a name, written the canonical way
	}
	return v, nil
}
