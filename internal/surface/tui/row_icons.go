package tui

import (
	"strings"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
)

// iconSlot is one cell of a row's icon strip. The order of the constants is
// the layout order; row_icon_set can remove slots but never reorder them.
type iconSlot int

const (
	slotUnseen iconSlot = iota
	slotReason
	slotLoop
	slotSubagents
	slotSpend
	slotLongRun
)

var iconSlotIDs = [...]string{"unseen", "reason", "loop", "subagents", "spend", "longrun"}

// id is the slot's name in the row_icon_set setting.
func (s iconSlot) id() string { return iconSlotIDs[s] }

// slotWidth is each slot's fixed cell width, indexed by iconSlot.
var slotWidth = [...]int{1, 8, 8, colAgents, 6, colRun}

// longRunGlyph is the long-run badge glyph (the same as retire_view's badge).
const longRunGlyph = "▴"

// Unicode and ASCII forms of the strip glyphs, by name.
var iconGlyphs = map[string][2]string{
	"unseen":    {"✦", "*"},
	"asks":      {"?", "?"},
	"review":    {"✓", "+"},
	"loop":      {loopDefault, "@"},
	"subagents": {subagentGlyph, "Y"},
	"spend":     {"$", "$"},
	"longrun":   {longRunGlyph, "^"},
}

// rowIconsOn reports whether rows render the icon strip: row_icons on and
// not the mobile layout.
func rowIconsOn(m Model) bool {
	return settings.FeatureOn(m.Store, "row_icons") && !mobileLayout(m)
}

// iconSlots are the strip's slots in layout order: the row_icon_set ids
// (unknown ones ignored), minus the loop slot with loops off and the spend
// slot with usage off.
func iconSlots(m Model) []iconSlot {
	raw := "unseen,reason,loop,subagents,spend,longrun"
	if m.Store != nil {
		if v, ok := m.Store.Get("row_icon_set").(string); ok {
			raw = v
		}
	}
	want := map[string]bool{}
	for _, id := range strings.Split(raw, ",") {
		want[strings.ToLower(strings.TrimSpace(id))] = true
	}
	var out []iconSlot
	for s := slotUnseen; s <= slotLongRun; s++ {
		if !want[s.id()] {
			continue
		}
		if s == slotLoop && !settings.FeatureOn(m.Store, "loops") {
			continue
		}
		if s == slotSpend && !settings.FeatureOn(m.Store, "usage") {
			continue
		}
		out = append(out, s)
	}
	return out
}

// iconGlyph is the strip glyph for name, in its ASCII form with ascii_glyphs
// on. An unknown name is "".
func iconGlyph(m Model, name string) string {
	g, ok := iconGlyphs[name]
	if !ok {
		return ""
	}
	if m.Store != nil {
		if on, _ := m.Store.Get("ascii_glyphs").(bool); on {
			return g[1]
		}
	}
	return g[0]
}
