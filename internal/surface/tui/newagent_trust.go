package tui

import (
	"path/filepath"
	"slices"

	"github.com/charmbracelet/x/ansi"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// The new-agent page's trust step (P2, R21): a radio list of what to do
// about the folder the agent starts in. Each choice that trusts something
// names the exact resolved folder under it. Only space, ctrl+t and ctrl+a
// change the choice; enter goes on to the next step and never picks, and a
// launch writes auto_trust_dirs only for an explicitly chosen "always".

// naShowDir is how the trust step names a folder: as it is. Tests swap it
// to keep a temp folder out of a golden file.
var naShowDir = func(dir string) string { return dir }

// naTrustChoices is the offer for the folder and the choices the step
// lists: none for a folder an auto_trust_dirs entry already covers, only
// "ask" for one that can never be trusted, else ask and this session, and
// always unless the folder is outside the home folder (nothing to save).
func naTrustChoices(m Model, st *NewAgentState) (trustOffer, []wizTrustMode) {
	o := trustOfferFor(m, spawnCWD(m))
	switch {
	case o.Covered != "":
		return o, nil
	case o.Refused != "":
		return o, []wizTrustMode{trustAsk}
	}
	rows := []wizTrustMode{trustAsk, trustSession}
	if o.AlwaysDir != "" {
		rows = append(rows, trustAlways)
	}
	return o, rows
}

// naClampTrust drops a trust choice the folder does not offer now: one made
// for another folder (the draft outlived a workspace or session switch), or
// one the folder cannot take (covered, refused, outside the home folder for
// "always"). The choice falls back to ask, so a stale one never reaches the
// launch.
func naClampTrust(m Model, st *NewAgentState) {
	if st.Trust == trustAsk {
		return
	}
	_, rows := naTrustChoices(m, st)
	if (st.TrustFor != "" && st.TrustFor != spawnCWD(m)) || !slices.Contains(rows, st.Trust) {
		st.Trust, st.TrustFor = trustAsk, ""
	}
}

// naSyncTrustCur puts the trust cursor on the current choice.
func naSyncTrustCur(m Model, st *NewAgentState) {
	naClampTrust(m, st)
	_, rows := naTrustChoices(m, st)
	st.TrustCur = max(slices.Index(rows, st.Trust), 0)
}

// naPickTrust is space on the trust step: the cursor's choice becomes the
// choice.
func naPickTrust(m Model) {
	st := m.NewAgent
	_, rows := naTrustChoices(m, st)
	if len(rows) == 0 {
		return
	}
	st.Trust = rows[min(max(st.TrustCur, 0), len(rows)-1)]
	st.TrustFor = spawnCWD(m)
	playNav(m)
}

// naTrustRadio is one radio row: the cursor, (•) or ( ) and the words, then
// the exact folder under it in at most maxDir lines (cut from the left when
// very long, so the project name stays), fw cells wide. The folder comes
// from disk, so it is flattened: an escape sequence in a folder name never
// reaches the terminal.
func naTrustRadio(cur, on bool, label, dir string, fw, maxDir int) []string {
	radio := styleDim.Render("( )")
	if on {
		radio = styleBootReady.Render("(•)")
	}
	text := stylePrimary.Render(label)
	if cur {
		text = selectedStyle(stylePrimary).Bold(true).Render(label)
	}
	out := []string{ansi.Truncate(naMarkCol(cur)+radio+" "+text, max(fw, 1), "…")}
	if dir == "" {
		return out
	}
	for _, part := range wrapPath(filepath.ToSlash(naShowDir(protocol.FlattenDisplayText(dir))), max(fw-6, 8), maxDir) {
		out = append(out, "      "+styleDim.Render(part))
	}
	return out
}

// naTrustBody is the trust step's body fw wide in at most rows rows: the
// radio list windowed so the cursor's radio, label and folder lines stay
// together and visible, or the lines that say why there is no list (the
// folder is covered, or can never be trusted).
func naTrustBody(m Model, st *NewAgentState, fw, rows int) []string {
	o, modes := naTrustChoices(m, st)
	switch {
	case o.Covered != "":
		return []string{"  " + styleDim.Render("trusted ("+trustSettingID+")")}
	case o.Refused != "":
		var out []string
		for _, l := range naWrap(protocol.FlattenDisplayText(o.Refused), max(fw-2, 8)) {
			out = append(out, "  "+styleBootWarn.Render(l))
		}
		return append(out, naTrustRadio(true, st.Trust == trustAsk, "ask: the agent will ask", "", fw, 0)...)
	}
	maxDir := 2
	if rows >= 12 {
		maxDir = 4 // room to show a long folder whole
	}
	var out []string
	start, end := 0, 0 // the cursor's block in out
	for i, mode := range modes {
		cur, on := i == st.TrustCur, st.Trust == mode
		var block []string
		switch mode {
		case trustAsk:
			block = naTrustRadio(cur, on, "ask: the agent will ask", "", fw, maxDir)
		case trustSession:
			block = naTrustRadio(cur, on, "this session only", o.SessionDir, fw, maxDir)
		case trustAlways:
			block = naTrustRadio(cur, on, "always: saves "+protocol.FlattenDisplayText(o.AlwaysEntry)+" to "+trustSettingID, o.AlwaysDir, fw, maxDir)
		}
		if cur {
			start, end = len(out), len(out)+len(block)
		}
		out = append(out, block...)
	}
	if o.AlwaysDir == "" {
		out = append(out, ansi.Truncate("  "+styleDim.Render("( ) always: only folders inside your home can be saved"), max(fw, 1), "…"))
	}
	if rows < 1 || len(out) <= rows {
		return out
	}
	// Window on the cursor's block: it shows whole when it fits.
	from := min(max(end-rows, 0), start)
	from = min(from, len(out)-rows)
	return out[from : from+rows]
}

// naTrustDone is the trust step's word on a transcript line: the choice, or
// "trusted" for a covered folder.
func naTrustDone(m Model, st *NewAgentState) string {
	if trustOfferFor(m, spawnCWD(m)).Covered != "" {
		return "trusted"
	}
	switch st.Trust {
	case trustSession:
		return "this session"
	case trustAlways:
		return "always"
	case trustAsk:
	}
	return "ask"
}

// naTrustSummary is the trust step's words on the summary.
func naTrustSummary(m Model, st *NewAgentState) string {
	o := trustOfferFor(m, spawnCWD(m))
	switch {
	case o.Covered != "":
		return "trusted (" + trustSettingID + ")"
	case o.Refused != "":
		return "ask: the agent will ask"
	}
	switch st.Trust {
	case trustSession:
		return "this session only"
	case trustAlways:
		return "always (" + protocol.FlattenDisplayText(o.AlwaysEntry) + ")"
	case trustAsk:
	}
	return "ask: the agent will ask"
}

// naTrustFolder is the exact folder a chosen trust covers, in full and
// wrapped to w cells, for the summary's trust row: nothing for ask or a
// covered folder, and nothing when the folder fits on the row's own line.
func naTrustFolder(m Model, st *NewAgentState, w int) []string {
	o, _ := naTrustChoices(m, st)
	dir := ""
	switch st.Trust {
	case trustSession:
		dir = o.SessionDir
	case trustAlways:
		dir = o.AlwaysDir
	case trustAsk:
	}
	dir = filepath.ToSlash(naShowDir(protocol.FlattenDisplayText(dir)))
	if dir == "" || ansi.StringWidth(dir) <= w {
		return nil
	}
	var out []string
	for _, part := range wrapPath(dir, max(w, 8), 1<<10) {
		out = append(out, styleDim.Render(part))
	}
	return out
}
