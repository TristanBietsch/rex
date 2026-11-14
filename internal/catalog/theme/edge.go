package theme

import (
	"fmt"
	"hash/fnv"
	"slices"
	"strings"
)

// EdgeColor is one swatch for a window edge (the TUI's border; R06 workspace
// edges reuse it). The ids are Pantone Color-of-the-Year inspired and the hex
// values are approximations, not official Pantone values; rex is not affiliated
// with Pantone. Each hue sits next to an Okabe-Ito (or IBM CVD-safe) role.
type EdgeColor struct {
	ID string
	// Ref names the Color of the Year the hue is inspired by.
	Ref string
	// Dark is used on dark themes, Light on light ones. Both are "#RRGGBB".
	Dark, Light string
	// ANSI is the terminal palette index used by the "ansi" scheme. It is
	// empty for a custom "#rrggbb" color.
	ANSI string
}

// EdgeColors lists every named edge color. Treat it as read-only.
var EdgeColors = []EdgeColor{
	{"tangerine", "17-1463 Tangerine Tango", "#DD4124", "#DD4124", "9"},
	{"mimosa", "14-0848 Mimosa", "#F0C05A", "#B18513", "3"},
	{"illuminating", "13-0647 Illuminating", "#F5DF4D", "#A18C00", "11"},
	{"emerald", "17-5641 Emerald", "#009473", "#009473", "2"},
	{"cerulean", "15-4020 Cerulean", "#9BB7D4", "#758FA9", "14"},
	{"classic-blue", "19-4052 Classic Blue", "#4F83C0", "#0F4C81", "12"},
	{"very-peri", "17-3938 Very Peri", "#6667AB", "#6667AB", "13"},
	{"orchid", "18-3224 Radiant Orchid", "#B163A3", "#B163A3", "5"},
	{"viva-magenta", "18-1750 Viva Magenta", "#BE3455", "#BE3455", "1"},
	{"ultimate-gray", "17-5104 Ultimate Gray", "#939597", "#8A8C8E", "8"},
}

// The random pools are the subsets whose colors stay far apart for people
// with normal vision and protan, deutan and tritan color vision. On paper,
// yellow cannot reach 3:1 contrast and stay distinct from mimosa.
var (
	darkEdgePool  = []string{"tangerine", "illuminating", "cerulean", "very-peri", "viva-magenta", "mimosa", "ultimate-gray"}
	lightEdgePool = []string{"mimosa", "cerulean", "very-peri", "viva-magenta", "classic-blue"}
)

func edgePool(dark bool) []string {
	if dark {
		return darkEdgePool
	}
	return lightEdgePool
}

// EdgeIDs returns the named edge color ids in table order.
func EdgeIDs() []string {
	ids := make([]string, len(EdgeColors))
	for i, c := range EdgeColors {
		ids[i] = c.ID
	}
	return ids
}

// EdgePool returns the ids PickEdge chooses from on a dark or light theme.
func EdgePool(dark bool) []string { return slices.Clone(edgePool(dark)) }

// Hex is the color to draw on a dark or light theme.
func (c EdgeColor) Hex(dark bool) string {
	if dark {
		return c.Dark
	}
	return c.Light
}

// poolColor looks up a pool id; pools only hold ids from EdgeColors.
func poolColor(id string) EdgeColor {
	for _, c := range EdgeColors {
		if c.ID == id {
			return c
		}
	}
	panic("theme: edge pool id not in EdgeColors: " + id)
}

// PickEdge picks a pool color for seed: the same seed always gets the same
// color. Ids in avoid are skipped; when every pool color is avoided, the
// plain pick is returned.
func PickEdge(seed string, dark bool, avoid []string) EdgeColor {
	pool := edgePool(dark)
	h := fnv.New32a()
	_, _ = h.Write([]byte(seed))
	start := uint64(h.Sum32()) % uint64(len(pool))
	for i := range uint64(len(pool)) {
		id := pool[(start+i)%uint64(len(pool))]
		if !slices.Contains(avoid, id) {
			return poolColor(id)
		}
	}
	return poolColor(pool[start])
}

// NextEdge returns the pool color after id (wrapping), for ":border next".
// An id outside the pool (a custom color, or a color only offered by name)
// starts at the first pool color.
func NextEdge(id string, dark bool) EdgeColor {
	pool := edgePool(dark)
	i := slices.Index(pool, id)
	return poolColor(pool[(i+1)%len(pool)]) // i == -1 gives pool[0]
}

// ParseEdge accepts a named edge color id or "#rgb" / "#rrggbb" (any case)
// and rejects everything else, so only a palette id or a validated hex ever
// reaches a terminal color. A hex color has ID "#rrggbb" (lowercase) and the
// same value for Dark and Light.
func ParseEdge(s string) (EdgeColor, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	for _, c := range EdgeColors {
		if c.ID == s {
			return c, nil
		}
	}
	h := s
	if len(h) == 4 && h[0] == '#' {
		h = "#" + string([]byte{h[1], h[1], h[2], h[2], h[3], h[3]})
	}
	if len(h) == 7 && h[0] == '#' && isLowerHex(h[1:]) {
		return EdgeColor{ID: h, Ref: "custom", Dark: h, Light: h}, nil
	}
	if len(s) > 32 {
		s = s[:32] + "..."
	}
	return EdgeColor{}, fmt.Errorf("unknown border color %q: use random, one of %s, or #rrggbb", s, strings.Join(EdgeIDs(), ", "))
}

func isLowerHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
