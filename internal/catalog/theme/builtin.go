package theme

// fleetColors is shared by every built-in: fleet names hash onto it, and
// these hues read on both dark and light terminals.
var fleetColors = []string{
	"#5B8DEF", "#E5B341", "#4ADE80", "#EF4444", "#A78BFA",
	"#22D3EE", "#FB923C", "#F472B6", "#84CC16", "#14B8A6",
}

// The built-ins carry the TUI's current values, so adopting this package
// changes no pixels. Accent and Queued equal Working, as the TUI paints both
// with it today; a scheme can give Accent its own hue later.
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

var noir = Palette{
	Name: "noir", Dark: true,
	Bg: "#05080F", Fg: "#D8DDE6", FgDim: "#6A7280", FgMuted: "#3A4150",
	BgSel: "#0A0D14", Border: "#1A1E28", BorderFocus: "#FFFFFF",
	Accent:  "#7AA6F0",
	Working: "#7AA6F0", Queued: "#7AA6F0", Needs: "#D4B574", Done: "#5BC487",
	Failed: "#D85A5A", Crashed: "#6A7280",
	Warn: "#D4B574", Danger: "#D85A5A", Success: "#5BC487", Info: "#7AA6F0",
	Fleet: fleetColors,
}

var paper = Palette{
	Name: "paper", Dark: false,
	Bg: "#FBFAF7", Fg: "#1A1A1A", FgDim: "#5C5C5C", FgMuted: "#999999",
	BgSel: "#F2F0EA", Border: "#D0CFC9", BorderFocus: "#1A1A1A",
	Accent:  "#2D5BB8",
	Working: "#2D5BB8", Queued: "#2D5BB8", Needs: "#B58800", Done: "#2C7A48",
	Failed: "#B83333", Crashed: "#999999",
	Warn: "#B58800", Danger: "#B83333", Success: "#2C7A48", Info: "#2D5BB8",
	Fleet: fleetColors,
}
