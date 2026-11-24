package settings

import (
	"strings"

	"github.com/tristanbietsch/rex/internal/catalog/theme"
)

// ScanColorAccent is the summary_scan_color value that paints the scanner in
// the live accent (accent_color, else the theme's own accent).
const ScanColorAccent = "accent"

// normalizeScanColor is the Normalize func of summary_scan_color: "accent"
// (also written "theme", the accent_color word for the same thing), a named
// accent color, or a color in any form theme.ParseAccent accepts. Like
// accent_color, a custom value is kept as typed and resolved at render time.
func normalizeScanColor(v string) (string, error) {
	t := strings.ToLower(strings.TrimSpace(v))
	if t == "" || t == ScanColorAccent {
		return ScanColorAccent, nil
	}
	n, err := normalizeAccentColor(t)
	if err != nil {
		return "", err
	}
	if n == theme.AccentDefault {
		return ScanColorAccent, nil
	}
	return n, nil
}
