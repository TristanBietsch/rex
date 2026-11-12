package tui

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/tristanbietsch/rex/internal/features/spawnctx"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// The preview (R16 mockup E): enter on a context row shows the item in the
// right pane: its name and size over the first lines of a paste, scrolled
// with j/k; a file's first lines (read once when the preview opens, at
// most 64 KiB and 200 lines, binary refused) or a folder's entries from the
// workspace index. e (or ctrl+e) edits a paste in $EDITOR, r renames it, x
// removes the item, esc goes back to the list.

// naPreviewKey is a key on the preview.
func naPreviewKey(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	st := m.NewAgent
	if st.ItemIdx >= len(st.Items) {
		st.Screen = naCompose
		return m, nil
	}
	if k.Paste {
		return m, nil // a paste never acts as a key here
	}
	it := st.Items[st.ItemIdx]
	switch {
	case k.Type == tea.KeyEsc || isRune(k, 'q') || isRune(k, 'h') || k.Type == tea.KeyLeft:
		st.Screen = naCompose
		naFocus(st, naContext)
		playNav(m)
	case k.Type == tea.KeyCtrlS:
		return launchNewAgent(m)
	case k.Type == tea.KeyDelete || isRune(k, 'x') || isRune(k, 'd'):
		removeItem(st, st.ItemIdx)
		st.Screen = naCompose
		naFocus(st, naContext)
	case isRune(k, 'r'):
		naStartRename(st)
	case k.Type == tea.KeyCtrlE || isRune(k, 'e'):
		if it.Kind == spawnctx.Paste {
			return m, editInEditor(st.ItemIdx, it.Text)
		}
	default:
		if mv := vimMotion(k.String(), &st.gPending); mv != navNone && mv != navPending {
			naPreviewScroll(m, mv)
		}
	}
	return m, nil
}

// naPreviewScroll scrolls the preview by mv (the shared navigator over its
// first line), clamped to the text.
func naPreviewScroll(m Model, mv navMove) {
	st := m.NewAgent
	if st.ItemIdx >= len(st.Items) {
		return
	}
	h := m.Height
	if h <= 0 {
		h = 32
	}
	last := naPreviewMaxScroll(m, st, st.Items[st.ItemIdx])
	st.PrevScroll = navTarget(mv, min(st.PrevScroll, last), last+1, naPreviewTextRows(h, 0))
}

// naPreviewLines is what the preview shows of an item, one entry a line,
// and how many more lines a file has past what was read.
func naPreviewLines(st *NewAgentState, it spawnctx.Item) ([]string, int) {
	switch it.Kind {
	case spawnctx.Paste:
		if body := strings.TrimRight(it.Text, "\n"); body != "" {
			return strings.Split(body, "\n"), 0
		}
		return nil, 0
	case spawnctx.File, spawnctx.Dir:
		if st.PrevErr != "" {
			return []string{st.PrevErr}, 0
		}
		return st.PrevLines, st.PrevMore
	case spawnctx.URL:
	}
	return []string{it.Name}, 0
}

// naPreviewMeta is the size words flush right of the preview's title.
func naPreviewMeta(it spawnctx.Item) string {
	switch it.Kind {
	case spawnctx.Paste:
		return naLines(spawnctx.Lines(it.Text)) + " · " + naBytes(it.Bytes)
	case spawnctx.File:
		return naBytes(it.Bytes)
	case spawnctx.Dir:
		return strconv.Itoa(it.Files) + " files"
	default:
		return it.Kind.String()
	}
}

// naPreviewTextRows is how many text rows the preview box has on a page h
// rows tall, with errLines error lines under it.
func naPreviewTextRows(h, errLines int) int {
	return max(1, pageBodyHeight(h)-3-errLines)
}

// naPreviewMaxScroll is the last first line that still fills the box.
func naPreviewMaxScroll(m Model, st *NewAgentState, it spawnctx.Item) int {
	h := m.Height
	if h <= 0 {
		h = 32
	}
	text, _ := naPreviewLines(st, it)
	return max(0, len(text)-naPreviewTextRows(h, 0))
}

// naPreviewBody is the preview fw wide for a body bh rows tall: title and
// size, the boxed text from PrevScroll ("… N more lines" when it goes on),
// then the page's error.
func naPreviewBody(m Model, st *NewAgentState, fw, bh int) []string {
	it := st.Items[st.ItemIdx]
	errs := naErrLines(st, fw)
	title := styleArrow.Render(strings.ToUpper(protocol.FlattenDisplayText(it.Name)))
	if st.Renaming {
		title = naRenameField(m, st)
	}
	meta := styleDim.Render(naPreviewMeta(it))
	if gap := fw - ansi.StringWidth(title) - ansi.StringWidth(meta); gap >= 2 {
		title += strings.Repeat(" ", gap) + meta
	}

	w := max(fw, 5)
	inner := w - 4
	rows := naPreviewTextRows(pageChromeRows+bh, len(errs))
	text, unread := naPreviewLines(st, it)
	start := min(st.PrevScroll, max(0, len(text)-rows))
	shown := text[start:]
	more := unread
	switch {
	case len(shown) > rows:
		more += len(shown) - (rows - 1)
		shown = shown[:rows-1]
	case unread > 0 && len(shown) == rows:
		more++ // room for the "more" line
		shown = shown[:rows-1]
	}
	border := styleBorderFg
	lines := []string{title, border.Render("┌" + strings.Repeat("─", w-2) + "┐")}
	box := func(s string) {
		lines = append(lines, border.Render("│ ")+padLine(s, inner)+border.Render(" │"))
	}
	for _, l := range shown {
		l = protocol.FlattenDisplayText(strings.ReplaceAll(l, "\t", "    "))
		box(stylePrimary.Render(ansi.Truncate(l, inner, "…")))
	}
	if more > 0 {
		box(styleMuted.Render(ansi.Truncate("… "+strconv.Itoa(more)+" more lines", inner, "…")))
	}
	for range rows - len(shown) - min(more, 1) {
		box("")
	}
	lines = append(lines, border.Render("└"+strings.Repeat("─", w-2)+"┘"))
	lines = append(lines, errs...)
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, fw, "…")
	}
	return lines
}

// naPreviewHints is the preview's footer.
func naPreviewHints(st *NewAgentState, narrow bool) [][2]string {
	if st.Renaming {
		return [][2]string{{"enter", "save"}, {"esc", "cancel"}}
	}
	paste := st.ItemIdx < len(st.Items) && st.Items[st.ItemIdx].Kind == spawnctx.Paste
	if paste && narrow {
		return [][2]string{{"e", "edit"}, {"j/k", "scroll"}, {"esc", "back"}}
	}
	if paste {
		return [][2]string{{"e", "edit"}, {"r", "rename"}, {"x", "remove"}, {"j/k", "scroll"}, {"esc", "back"}}
	}
	return [][2]string{{"x", "remove"}, {"esc", "back"}}
}
