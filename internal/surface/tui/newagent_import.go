package tui

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/tristanbietsch/rex/internal/features/spawnctx"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// Workspace files and folders as context (R16 mockups D and F, D9). When
// the page opens, a tea.Cmd lists the session folder (spawnctx.Walk: git
// ls-files in a repo, else a plain walk; 10,000 entries or 300 ms) off the
// UI goroutine; closing the page cancels it. ctrl+o opens the import
// picker over that index: type to fuzzy-match, space marks, enter adds the
// marked rows (or the cursor row), → opens a folder and ← goes up. @ at a
// word start in the task editor opens a six-row completion list under the
// editor: enter or tab puts @path in the task and adds the item, esc
// closes the list and keeps the typed text. Every path goes through
// spawnctx.ValidatePath when it is added; a denylisted one waits for y.

// The walk's caps and the preview's (spec limits table).
const (
	naWalkMax      = 10000
	naWalkBudget   = 300 * time.Millisecond
	naPreviewBytes = 64 << 10
	naPreviewMax   = 200
)

// naCompleteRows is the @ completion list's height; naImportLimit caps the
// picker's matches.
const (
	naCompleteRows = 6
	naImportLimit  = 200
)

// walkWorkspace lists the workspace; tests swap it.
var walkWorkspace = spawnctx.WalkCounted

// indexGens numbers walks across pages, so a closed page's late result is
// never taken for a new page's.
var indexGens atomic.Int64

// indexDoneMsg is a finished walk: Gen tells which page started it.
type indexDoneMsg struct {
	Gen   int64
	Index spawnctx.Index
	Err   error
}

// startIndex starts listing root for the page: the returned command runs
// the walk (never on the UI goroutine) and its context is cancelled when
// the page closes (stopIndex).
func startIndex(st *NewAgentState, root string) tea.Cmd {
	stopIndex(st)
	ctx, cancel := context.WithCancel(context.Background())
	count := new(atomic.Int64)
	gen := indexGens.Add(1)
	st.Root, st.Index, st.Indexing, st.IndexErr = root, spawnctx.Index{}, true, nil
	st.indexCount, st.indexGen, st.indexCancel = count, gen, cancel
	return func() tea.Msg {
		idx, err := walkWorkspace(ctx, root, naWalkMax, naWalkBudget, count)
		return indexDoneMsg{Gen: gen, Index: idx, Err: err}
	}
}

// stopIndex cancels the page's walk, if one runs.
func stopIndex(st *NewAgentState) {
	if st.indexCancel != nil {
		st.indexCancel()
		st.indexCancel = nil
	}
}

// handleIndexDone takes the walk's result, if it is the open page's.
func handleIndexDone(m Model, msg indexDoneMsg) (Model, tea.Cmd) {
	st := m.NewAgent
	if st == nil || msg.Gen != st.indexGen {
		return m, nil
	}
	st.Index, st.IndexErr, st.Indexing = msg.Index, msg.Err, false
	st.rowsKey = ""
	st.Rev++
	return m, nil
}

// errNeedConfirm is addPath's answer for a denylisted path not yet
// confirmed.
var errNeedConfirm = errors.New("needs a confirm")

// naItemsCap is the context item cap's error.
func naItemsCap() error {
	return naErr{msg: strconv.Itoa(spawnctx.DefaultCaps.Items) + " context items is the limit; remove one first."}
}

// addPath adds workspace path rel (a folder may end in "/") as a file or
// dir item, once: a path already in the list is left alone. The path rules
// (spawnctx.ValidatePath) refuse it with the page's words; a denylisted
// path is errNeedConfirm until confirmed; a file over 1 MiB is added with a
// warning on the page.
func addPath(st *NewAgentState, rel string, confirmed bool) error {
	rel = strings.TrimSuffix(rel, "/")
	info, err := spawnctx.ValidatePath(st.Root, rel)
	if err != nil {
		return naPathErr(st.Root, rel, err)
	}
	kind := spawnctx.File
	if info.Dir {
		kind = spawnctx.Dir
	}
	for _, it := range st.Items {
		if it.Kind == kind && it.Name == info.Rel {
			return nil
		}
	}
	if len(st.Items) >= spawnctx.DefaultCaps.Items {
		return naItemsCap()
	}
	if info.Deny != "" && !confirmed {
		return errNeedConfirm
	}
	it := spawnctx.Item{Kind: kind, Name: info.Rel}
	if info.Dir {
		it.Files = spawnctx.DirFiles(st.Index, info.Rel)
	} else {
		it.Bytes = int(info.Size)
	}
	st.Items = append(st.Items, it)
	if info.Warn != "" {
		st.Err = "! " + info.Rel + " is " + naBytes(it.Bytes) + ": " + info.Warn
	}
	return nil
}

// naPathErr is a refused path in the page's words (mockup F).
func naPathErr(root, rel string, err error) error {
	where := tildePath(root)
	var pe *spawnctx.PathError
	var msg string
	switch {
	case errors.Is(err, spawnctx.ErrOutside) && errors.As(err, &pe) && pe.Target != "":
		msg = rel + " links to " + tildePath(pe.Target) + ", outside " + where + ": only workspace files can be added."
	case errors.Is(err, spawnctx.ErrOutside):
		msg = rel + " is outside " + where + ": only workspace files can be added."
	case errors.Is(err, spawnctx.ErrSpecial):
		msg = rel + " is not a regular file or folder: it was not added."
	case errors.Is(err, spawnctx.ErrBadName):
		msg = strconv.Quote(rel) + " has control characters or line breaks in its name: it was not added."
	case errors.Is(err, fs.ErrNotExist):
		msg = rel + " does not exist (any more)."
	default:
		msg = "cannot add " + rel + ": " + err.Error()
	}
	return naErr{msg: msg}
}

// naImport is the open picker: the query, the folder browsed ("" for the
// root, else ending in "/"), the cursor, the marked rows in marking order,
// and whether the last enter added anything (the picker then closes once
// its confirms are answered).
type naImport struct {
	Query  []rune
	Dir    string
	Cur    int
	Marked []string
	added  bool
}

// naPending is a denylisted path waiting for y. For an @ completion, Row,
// From and To are the typed "@text" that becomes "@path" once confirmed;
// Row is -1 from the picker.
type naPending struct {
	Rel           string
	Row, From, To int
}

// openImport is ctrl+o: the picker, afresh, with the rail on Context.
func openImport(m Model) Model {
	st := m.NewAgent
	st.Import = &naImport{}
	st.Screen, st.Section, st.Complete = naImportScreen, naContext, nil
	st.rowsKey = ""
	playNav(m)
	return m
}

// closeImport goes back to Compose.
func closeImport(st *NewAgentState) {
	st.Screen, st.Import = naCompose, nil
}

// importRows is what the picker lists: with no query the folder's entries
// (folders first), else the matches under the folder, best first. Nothing
// while the walk runs. The rows are cached until the query, folder or
// index changes.
func (st *NewAgentState) importRows() []string {
	im := st.Import
	if im == nil || st.Indexing {
		return nil
	}
	key := im.Dir + "\x00" + string(im.Query)
	if st.rowsKey == key {
		return st.rowsCache
	}
	var rows []string
	if len(im.Query) == 0 {
		rows = spawnctx.Children(st.Index, im.Dir)
	} else {
		idx := st.Index
		if im.Dir != "" {
			var under []string
			for _, p := range idx.Paths {
				if p != im.Dir && strings.HasPrefix(p, im.Dir) {
					under = append(under, p)
				}
			}
			idx = spawnctx.Index{Paths: under}
		}
		rows = spawnctx.Match(idx, string(im.Query), naImportLimit)
	}
	st.rowsKey, st.rowsCache = key, rows
	return rows
}

// naImportKey is a key in the picker.
func naImportKey(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	st := m.NewAgent
	im := st.Import
	if im == nil {
		closeImport(st)
		return m, nil
	}
	if k.Paste {
		// A paste types into the query, on one line.
		im.Query = append(im.Query, []rune(strings.Join(strings.Fields(spawnctx.Sanitize(string(k.Runes))), " "))...)
		im.Cur = 0
		return m, nil
	}
	rows := st.importRows()
	row := ""
	if im.Cur < len(rows) {
		row = rows[im.Cur]
	}
	switch k.Type {
	case tea.KeyEsc:
		closeImport(st)
		playNav(m)
	case tea.KeyCtrlS:
		closeImport(st)
		return launchNewAgent(m)
	case tea.KeyUp, tea.KeyCtrlP:
		im.Cur--
	case tea.KeyDown, tea.KeyCtrlN:
		im.Cur++
	case tea.KeyRight:
		if strings.HasSuffix(row, "/") {
			im.Dir, im.Query, im.Cur = row, nil, 0
		}
	case tea.KeyLeft:
		if im.Dir != "" {
			from := im.Dir
			parent := strings.TrimSuffix(from, "/")
			if i := strings.LastIndexByte(parent, '/'); i >= 0 {
				im.Dir = parent[:i+1]
			} else {
				im.Dir = ""
			}
			im.Query, im.Cur = nil, 0
			for i, r := range st.importRows() {
				if r == from {
					im.Cur = i
				}
			}
		}
	case tea.KeySpace:
		if row != "" {
			if i := slices.Index(im.Marked, row); i >= 0 {
				im.Marked = append(im.Marked[:i], im.Marked[i+1:]...)
			} else {
				im.Marked = append(im.Marked, row)
			}
		}
	case tea.KeyEnter:
		naImportAdd(st, row)
	case tea.KeyBackspace:
		if n := len(im.Query); n > 0 {
			im.Query, im.Cur = im.Query[:n-1], 0
		}
	case tea.KeyCtrlU:
		im.Query, im.Cur = nil, 0
	case tea.KeyRunes:
		if !k.Alt {
			im.Query = append(im.Query, []rune(spawnctx.Sanitize(string(k.Runes)))...)
			im.Cur = 0
		}
	}
	im.Cur = min(max(im.Cur, 0), max(len(st.importRows())-1, 0))
	return m, nil
}

// naImportAdd is enter in the picker: the marked rows, or the cursor row,
// are added in order. A refused path's error shows; a denylisted one waits
// for y. Once something was added and nothing waits, the picker closes.
func naImportAdd(st *NewAgentState, row string) {
	im := st.Import
	targets := im.Marked
	if len(targets) == 0 && row != "" {
		targets = []string{row}
	}
	im.Marked, im.added = nil, false
	for _, rel := range targets {
		switch err := addPath(st, rel, false); {
		case errors.Is(err, errNeedConfirm):
			st.Pending = append(st.Pending, naPending{Rel: rel, Row: -1})
		case err != nil:
			if !strings.HasPrefix(st.Err, "✗") {
				st.Err = naFail(err) // the first error shows
			}
		default:
			im.added = true
		}
	}
	naImportSettle(st)
}

// naImportSettle closes the picker for Compose's context list once nothing
// waits for a confirm and something was added.
func naImportSettle(st *NewAgentState) {
	if st.Screen == naImportScreen && st.Import != nil && len(st.Pending) == 0 && st.Import.added {
		closeImport(st)
		st.Section = naContext
	}
}

// naPendingKey answers the first denylisted path waiting: y adds it (and,
// from @, puts @path in the task); any other key skips it.
func naPendingKey(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	st := m.NewAgent
	p := st.Pending[0]
	st.Pending = st.Pending[1:]
	if isRune(k, 'y') {
		if err := addPath(st, p.Rel, true); err != nil {
			st.Err = naFail(err)
		} else {
			if p.Row >= 0 {
				st.Task.ReplaceSpan(p.Row, p.From, p.To, "@"+p.Rel)
			}
			if st.Import != nil {
				st.Import.added = true
			}
		}
	}
	naImportSettle(st)
	return m, nil
}

// naComplete is the open @ completion: the "@" is at rune Col of task line
// Row; Cur is the list cursor.
type naComplete struct {
	Row, Col, Cur int
}

// completeQuery is the text typed after the "@", ok false once the
// completion no longer applies: the cursor left the line or went before
// the "@", the "@" is gone, or a space ended the word.
func (st *NewAgentState) completeQuery() (string, bool) {
	cp, c := st.Complete, &st.Task
	if cp == nil || c.Row != cp.Row || cp.Row >= len(c.Lines) {
		return "", false
	}
	line := c.Lines[cp.Row]
	if cp.Col >= len(line) || line[cp.Col] != '@' || c.Col <= cp.Col || c.Col > len(line) {
		return "", false
	}
	q := string(line[cp.Col+1 : c.Col])
	if strings.ContainsFunc(q, unicode.IsSpace) {
		return "", false
	}
	return q, true
}

// completeRows is the completion list for q: nothing while the walk runs.
func (st *NewAgentState) completeRows(q string) []string {
	if st.Indexing {
		return nil
	}
	return spawnctx.Match(st.Index, q, naCompleteRows)
}

// naSyncComplete follows a task editor key: an "@" typed at a word start
// opens the completion; once the typed text no longer fits, it closes.
func naSyncComplete(st *NewAgentState, k tea.KeyMsg) {
	c := &st.Task
	if st.Complete == nil {
		if !isRune(k, '@') || c.Row >= len(c.Lines) {
			return
		}
		line, at := c.Lines[c.Row], c.Col-1
		if at >= 0 && at < len(line) && line[at] == '@' && (at == 0 || unicode.IsSpace(line[at-1])) {
			st.Complete = &naComplete{Row: c.Row, Col: at}
		}
		return
	}
	if _, ok := st.completeQuery(); !ok {
		st.Complete = nil
		return
	}
	st.Complete.Cur = 0
}

// naCompleteKey is a key while the completion is open; ok false leaves it
// to the editor.
func naCompleteKey(m Model, k tea.KeyMsg) (Model, tea.Cmd, bool) {
	st := m.NewAgent
	cp := st.Complete
	q, ok := st.completeQuery()
	if !ok {
		st.Complete = nil
		return m, nil, false
	}
	rows := st.completeRows(q)
	switch {
	case k.Type == tea.KeyEsc:
		st.Complete = nil
	case k.Type == tea.KeyUp || k.Type == tea.KeyCtrlP:
		cp.Cur = max(cp.Cur-1, 0)
	case k.Type == tea.KeyDown || k.Type == tea.KeyCtrlN:
		cp.Cur = min(cp.Cur+1, max(len(rows)-1, 0))
	case (k.Type == tea.KeyEnter && !k.Alt) || k.Type == tea.KeyTab:
		st.Complete = nil
		if len(rows) == 0 {
			break // nothing to take: the list closes, nothing launches
		}
		rel, c := rows[min(cp.Cur, len(rows)-1)], &st.Task
		switch err := addPath(st, rel, false); {
		case errors.Is(err, errNeedConfirm):
			st.Pending = append(st.Pending, naPending{Rel: rel, Row: cp.Row, From: cp.Col, To: c.Col})
		case err != nil:
			st.Err = naFail(err)
		default:
			c.ReplaceSpan(cp.Row, cp.Col, c.Col, "@"+rel)
		}
	default:
		return m, nil, false
	}
	return m, nil, true
}

// naLoadPreview reads what the preview of the item under the cursor shows:
// a file's head (bounded, sanitized, binary refused) or a folder's entries
// from the index.
func naLoadPreview(st *NewAgentState) {
	st.PrevLines, st.PrevMore, st.PrevErr = nil, 0, ""
	if st.ItemIdx >= len(st.Items) {
		return
	}
	switch it := st.Items[st.ItemIdx]; it.Kind {
	case spawnctx.File:
		lines, more, err := spawnctx.Preview(st.Root, it.Name, naPreviewBytes, naPreviewMax)
		if err != nil {
			st.PrevErr = "✗ " + err.Error()
			return
		}
		st.PrevLines, st.PrevMore = lines, more
	case spawnctx.Dir:
		if st.Indexing {
			st.PrevErr = "the workspace is still being indexed"
			return
		}
		st.PrevLines = spawnctx.Children(st.Index, it.Name)
	case spawnctx.Paste, spawnctx.URL:
	}
}

// naThousands is n with thousands separators: "10,000".
func naThousands(n int) string {
	s := strconv.Itoa(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// naSpinner is the app spinner's current frame.
func naSpinner(m Model) string {
	frames := spinnerFramesFor(m)
	return frames[m.SpinnerTick%len(frames)]
}

// naIndexingLine is the loading state (mockup F): the spinner and the walk's
// count so far.
func naIndexingLine(m Model, st *NewAgentState) string {
	n := 0
	if st.indexCount != nil {
		n = int(st.indexCount.Load())
	}
	return styleArrow.Render(naSpinner(m)) + styleDim.Render(" indexing workspace… "+naThousands(n)+" files")
}

// naIndexedWords is "N files indexed" ("10,000+" when the walk stopped at
// a cap).
func naIndexedWords(st *NewAgentState) string {
	n := naThousands(len(st.Index.Paths))
	if st.Index.Truncated {
		n += "+"
	}
	return n + " files indexed"
}

// naPathMeta is a row's words flush right: "dir", a file's size, or a
// denylisted path's reason (naDenyMeta).
func naPathMeta(st *NewAgentState, rel string) string {
	if reason, ok := spawnctx.Denied(rel); ok {
		return reason + ": skip"
	}
	if strings.HasSuffix(rel, "/") {
		return "dir"
	}
	if st.sizes == nil {
		st.sizes = map[string]string{}
	}
	if s, ok := st.sizes[rel]; ok {
		return s
	}
	s := ""
	// Lstat only: a listed path is never opened to show its size.
	if fi, err := os.Lstat(filepath.Join(st.Root, filepath.FromSlash(rel))); err == nil && fi.Mode().IsRegular() {
		s = naBytes(int(fi.Size()))
	}
	st.sizes[rel] = s
	return s
}

// naPathRow is one picker or completion row fw wide: the cursor, the mark
// box (or "!" for a denylisted path), the path and its words flush right.
func naPathRow(st *NewAgentState, rel string, cur, box, marked bool, fw int) string {
	left := "  "
	label := protocol.FlattenDisplayText(rel)
	name := stylePrimary.Render(label)
	if cur {
		left = styleArrow.Render("▸ ")
		name = selectedStyle(stylePrimary).Bold(true).Render(label)
	}
	_, denied := spawnctx.Denied(rel)
	switch {
	case marked:
		left += styleArrow.Render("[x]") + " "
	case denied:
		left += styleBootWarn.Render("!") + " "
	case box:
		left += styleMuted.Render("[ ]") + " "
	}
	left += name
	metas := []string{naPathMeta(st, rel)}
	metaStyle := styleDim
	if denied {
		// Shorter words where the row is narrow: the reason never drops
		// to the color alone.
		metaStyle = styleBootWarn
		metas = append(metas, strings.TrimSuffix(metas[0], ": skip"), "secrets")
	}
	for _, meta := range metas {
		if gap := fw - ansi.StringWidth(left) - ansi.StringWidth(meta); meta != "" && gap >= 2 {
			left += strings.Repeat(" ", gap) + metaStyle.Render(meta)
			break
		}
	}
	return ansi.Truncate(left, fw, "…")
}

// naPendingLine asks about the first denylisted path waiting.
func naPendingLine(st *NewAgentState) string {
	p := st.Pending[0]
	reason, _ := spawnctx.Denied(p.Rel)
	return styleBootWarn.Render("! "+protocol.FlattenDisplayText(strings.TrimSuffix(p.Rel, "/"))+" "+reason+".") +
		styleDim.Render(" y adds it anyway.")
}

// naImportBody is the picker fw wide for a body bh rows tall (mockup D):
// the folder and the index size, the query, then the rows windowed on the
// cursor, or the loading state; the confirm and the page's error under it.
func naImportBody(m Model, st *NewAgentState, fw, bh int) []string {
	im := st.Import
	if im == nil {
		im = &naImport{}
	}
	where := tildePath(st.Root)
	if im.Dir != "" {
		where += "/" + strings.TrimSuffix(im.Dir, "/")
	}
	title := styleArrow.Render("IMPORT") + "  " + styleDim.Render(protocol.FlattenDisplayText(where))
	if !st.Indexing && st.IndexErr == nil {
		count := styleDim.Render(naIndexedWords(st))
		if gap := fw - ansi.StringWidth(title) - ansi.StringWidth(count); gap >= 2 {
			title += strings.Repeat(" ", gap) + count
		}
	}
	query := styleArrow.Render("> ") + stylePrimary.Render(protocol.FlattenDisplayText(string(im.Query))) + cursorBlock(m)
	lines := []string{title, query}
	errs := naErrLines(st)
	room := max(bh-len(lines)-len(errs), 1)
	switch {
	case st.Indexing:
		line := naIndexingLine(m, st)
		after := styleMuted.Render("(picker usable after)")
		if gap := fw - ansi.StringWidth(line) - ansi.StringWidth(after); gap >= 2 {
			line += strings.Repeat(" ", gap) + after
		}
		lines = append(lines, line)
	case st.IndexErr != nil:
		lines = append(lines, styleStateFailed.Render("✗ the workspace could not be listed: "+protocol.FlattenDisplayText(st.IndexErr.Error())))
	default:
		rows := st.importRows()
		if len(rows) == 0 {
			word := "nothing here"
			if len(im.Query) > 0 {
				word = "no match"
			}
			lines = append(lines, "  "+styleMuted.Render(word))
		}
		start := 0
		if len(rows) > room {
			start = min(max(im.Cur-room/2, 0), len(rows)-room)
		}
		for i := start; i < min(start+room, len(rows)); i++ {
			lines = append(lines, naPathRow(st, rows[i], i == im.Cur, true, slices.Index(im.Marked, rows[i]) >= 0, fw))
		}
	}
	lines = append(lines, errs...)
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, fw, "…")
	}
	return lines
}

// naImportHints is the picker's footer.
func naImportHints(st *NewAgentState, narrow bool) [][2]string {
	if len(st.Pending) > 0 {
		return naPendingHints()
	}
	if st.Indexing {
		return [][2]string{{"type", "to filter"}, {"esc", "back"}}
	}
	n := 1
	if st.Import != nil && len(st.Import.Marked) > 0 {
		n = len(st.Import.Marked)
	}
	add := "add " + strconv.Itoa(n)
	if narrow {
		return [][2]string{{"space", "mark"}, {"enter", add}, {"esc", "back"}}
	}
	return [][2]string{{"space", "mark"}, {"enter", add}, {"→", "open dir"}, {"←", "up"}, {"esc", "back"}}
}

// naPendingHints is the footer while a denylisted path waits for y.
func naPendingHints() [][2]string {
	return [][2]string{{"y", "add anyway"}, {"any other key", "skip"}}
}

// naCompleteLines is the @ completion list under the task editor, fw wide:
// up to naCompleteRows matches, the loading state, or "no match"; nil when
// it is closed.
func naCompleteLines(m Model, st *NewAgentState, fw int) []string {
	if st.Complete == nil || st.Section != naTask {
		return nil
	}
	q, ok := st.completeQuery()
	if !ok {
		return nil
	}
	if st.Indexing {
		return []string{"  " + naIndexingLine(m, st)}
	}
	rows := st.completeRows(q)
	if len(rows) == 0 {
		return []string{"  " + styleMuted.Render(ansi.Truncate("no match: esc keeps @"+protocol.FlattenDisplayText(q), max(fw-2, 1), "…"))}
	}
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = naPathRow(st, r, i == st.Complete.Cur, false, false, fw)
	}
	return out
}
