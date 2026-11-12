package theme

// fleetColors is shared by every hex built-in: fleet names hash onto it, and
// these hues read on both dark and light terminals.
var fleetColors = []string{
	"#5B8DEF", "#E5B341", "#4ADE80", "#EF4444", "#A78BFA",
	"#22D3EE", "#FB923C", "#F472B6", "#84CC16", "#14B8A6",
}

// classic is the look rex shipped with before the violet accent: the old
// default's values, kept for anyone who prefers them. Accent and Queued equal
// Working, as that TUI painted them.
var classic = Palette{
	Name: "classic", Dark: true,
	Bg: "#0F1115", Fg: "#E6E6E6", FgDim: "#7A7F8C", FgMuted: "#4A4F5A",
	BgSel: "#171922", Border: "#262A36", BorderFocus: "#FFFFFF",
	Accent:  "#5B8DEF",
	Working: "#5B8DEF", Queued: "#5B8DEF", Needs: "#E5B341", Done: "#4ADE80",
	Failed: "#EF4444", Crashed: "#7A7F8C",
	Warn: "#E5B341", Danger: "#EF4444", Success: "#4ADE80", Info: "#5B8DEF",
	Fleet: fleetColors,
}

// modern is the default. Violet is the accent because blue, amber, green and
// red already mean states: "the cursor is here" must not read as "working".
var modern = Palette{
	Name: "default", Dark: true,
	Bg: "#0F1115", Fg: "#E6E8EE", FgDim: "#8B91A1", FgMuted: "#5A6070",
	BgSel: "#1D212C", Border: "#2B303C", BorderFocus: "#AEB4C2",
	Accent:  "#B392F0",
	Working: "#6EA8FE", Queued: "#6EA8FE", Needs: "#F5B94A", Done: "#5BD08C",
	Failed: "#F26B6B", Crashed: "#8B91A1",
	Warn: "#F5B94A", Danger: "#F26B6B", Success: "#5BD08C", Info: "#6EA8FE",
	Fleet: fleetColors,
}

var noir = Palette{
	Name: "noir", Dark: true,
	Bg: "#05080F", Fg: "#D8DDE6", FgDim: "#7A8291", FgMuted: "#434A59",
	BgSel: "#121722", Border: "#1C212C", BorderFocus: "#9AA3B2",
	Accent:  "#9C8CF0",
	Working: "#7AA6F0", Queued: "#7AA6F0", Needs: "#D4B574", Done: "#5BC487",
	Failed: "#D85A5A", Crashed: "#7A8291",
	Warn: "#D4B574", Danger: "#D85A5A", Success: "#5BC487", Info: "#7AA6F0",
	Fleet: fleetColors,
}

var paper = Palette{
	Name: "paper", Dark: false,
	Bg: "#FBFAF7", Fg: "#1C1D21", FgDim: "#5D6270", FgMuted: "#9A9EA8",
	BgSel: "#EEECE6", Border: "#D9D7D0", BorderFocus: "#3A3D45",
	Accent:  "#6E4BD8",
	Working: "#2B5CC0", Queued: "#2B5CC0", Needs: "#966200", Done: "#1F7A45",
	Failed: "#C23B3B", Crashed: "#6E7380",
	Warn: "#966200", Danger: "#C23B3B", Success: "#1F7A45", Info: "#2B5CC0",
	Fleet: fleetColors,
}

var highContrast = Palette{
	Name: "high-contrast", Dark: true,
	Bg: "#000000", Fg: "#FFFFFF", FgDim: "#C8C8C8", FgMuted: "#9A9A9A",
	BgSel: "#303030", Border: "#808080", BorderFocus: "#FFFFFF",
	Accent:  "#00E5FF",
	Working: "#4DA3FF", Queued: "#4DA3FF", Needs: "#FFD000", Done: "#3CF07A",
	Failed: "#FF5C5C", Crashed: "#C8C8C8",
	Warn: "#FFD000", Danger: "#FF5C5C", Success: "#3CF07A", Info: "#4DA3FF",
	Fleet: fleetColors,
}

// ansi uses only the terminal's own six normal hues, so a Solarized or
// Gruvbox terminal theme gives rex a matching look, and it is the natural
// fallback on a 16-color terminal. Text, background and the neutral tones are
// the terminal's defaults (""): dim and muted text and borders are the default
// color drawn faint (Faint), because no fixed index is safe for them. Index 8
// is the Solarized Dark background, and the bright indexes are grays there.
// The selection is reverse video.
var ansi = Palette{
	Name: "ansi", Dark: true, Faint: true,
	Accent:  "5",
	Working: "4", Queued: "4", Needs: "3", Done: "2", Failed: "1",
	Warn: "3", Danger: "1", Success: "2", Info: "4",
	Fleet:      []string{"4", "3", "2", "1", "5", "6"},
	SelReverse: true,
}
