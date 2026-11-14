package settings

import (
	"strings"

	"github.com/tristanbietsch/rex/internal/catalog/theme"
)

// normalizeBorderColor is the Normalize func of window_border_color: "random",
// a named edge color or "#rgb" / "#rrggbb". Anything else is refused, so only
// a palette id or a validated hex ever reaches a terminal color.
func normalizeBorderColor(v string) (string, error) {
	if strings.EqualFold(strings.TrimSpace(v), "random") {
		return "random", nil
	}
	c, err := theme.ParseEdge(v)
	if err != nil {
		return "", err
	}
	return c.ID, nil
}
