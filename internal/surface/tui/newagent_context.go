package tui

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tristanbietsch/rex/internal/features/spawnctx"
)

// The new-agent page's context (R16 mockups B and E): a bracketed paste of
// more than naChipLines lines or naChipBytes bytes becomes a "paste N" item
// with a one-line "[paste N · L lines]" placeholder in the task (D5). The
// Context section lists the items; j/k move, enter previews, x removes, r
// renames. e (or ctrl+e) in a paste's preview, and ctrl+e in the task
// editor, edit the text in $EDITOR through a private temp file that is
// removed when the editor exits (D6). Nothing else ever writes the draft.

// The big-paste threshold (D5): more than this many lines or bytes.
const (
	naChipLines = 8
	naChipBytes = 1 << 10
)

// naRenameMax caps a name being typed, in runes.
const naRenameMax = 64

// naEditorMax caps what is read back from $EDITOR.
const naEditorMax = 1 << 20

// naSaveHint follows a too-big paste's error (mockup F).
const naSaveHint = "Save it to a file in the workspace and import it (ctrl+o)."

// naErr is an error in the page's own words, shown after "✗ ". tooBig marks
// a size cap, which the page follows with naSaveHint.
type naErr struct {
	msg    string
	tooBig bool
}

func (e naErr) Error() string { return e.msg }

func (e naErr) Unwrap() error {
	if e.tooBig {
		return spawnctx.ErrTooBig
	}
	return nil
}

// naFail is the page's error line for err: "✗ " and the words, plus the
// save hint under a size cap.
func naFail(err error) string {
	msg := "✗ " + err.Error()
	var ne naErr
	if errors.As(err, &ne) && ne.tooBig {
		msg += "\n" + naSaveHint
	}
	return msg
}

// pastePlaceholder is the paste's one line in the task. L counts the
// sanitized text as the prompt's lines attribute does (spawnctx.Lines).
func pastePlaceholder(it spawnctx.Item) string {
	return "[" + it.Name + " · " + naLines(spawnctx.Lines(it.Text)) + "]"
}

// pasteBytes is the paste text of items, leaving out index skip.
func pasteBytes(items []spawnctx.Item, skip int) int {
	n := 0
	for i, it := range items {
		if i != skip && it.Kind == spawnctx.Paste {
			n += it.Bytes
		}
	}
	return n
}

// checkPaste sanitizes text for the paste called name (index i of the
// items, or -1 for a new one) and checks the paste caps. It returns the
// clean text and its size as the prompt counts it.
func checkPaste(items []spawnctx.Item, i int, name, text string) (string, int, error) {
	if spawnctx.IsBinary(text) {
		return "", 0, naErr{msg: name + " looks like binary data, not text: it was not added"}
	}
	clean := spawnctx.Sanitize(text)
	size := len(strings.TrimRight(clean, "\n"))
	c := spawnctx.DefaultCaps
	if size+pasteBytes(items, i) > c.PasteTotal {
		return "", 0, naErr{tooBig: true, msg: fmt.Sprintf("%s is %s; the limit for pasted text is %s in total.",
			name, naBytes(size), naBytes(c.PasteTotal))}
	}
	if size > c.Paste {
		return "", 0, naErr{tooBig: true, msg: fmt.Sprintf("%s is %s; the limit for one paste is %s.",
			name, naBytes(size), naBytes(c.Paste))}
	}
	return clean, size, nil
}

// addPaste takes a bracketed paste: sanitized, it goes into the task at the
// cursor, or, over naChipLines lines or naChipBytes bytes, becomes the next
// "paste N" item with its placeholder in the task. A binary paste or one
// over a cap changes nothing and is an error.
func addPaste(s *NewAgentState, text string) error {
	if spawnctx.IsBinary(text) {
		return naErr{msg: "the paste looks like binary data, not text: it was not added"}
	}
	clean := spawnctx.Sanitize(text)
	if spawnctx.Lines(clean) <= naChipLines && len(clean) <= naChipBytes {
		s.Task.Insert(clean)
		return nil
	}
	if strings.TrimSpace(clean) == "" {
		return nil // nothing to keep
	}
	if len(s.Items) >= spawnctx.DefaultCaps.Items {
		return naErr{msg: strconv.Itoa(spawnctx.DefaultCaps.Items) + " context items is the limit; remove one first."}
	}
	seq := s.PasteSeq + 1
	for naNameTaken(s.Items, -1, "paste "+strconv.Itoa(seq)) {
		seq++ // a rename may have taken the next number
	}
	name := "paste " + strconv.Itoa(seq)
	clean, size, err := checkPaste(s.Items, -1, name, clean)
	if err != nil {
		return err
	}
	s.PasteSeq = seq
	it := spawnctx.Item{Kind: spawnctx.Paste, Name: name, Text: clean, Bytes: size}
	s.Items = append(s.Items, it)
	s.Task.Insert(pastePlaceholder(it))
	return nil
}

// setPaste replaces paste i's text (an $EDITOR edit), checked as a new
// paste would be, and updates its placeholder in the task.
func setPaste(s *NewAgentState, i int, text string) error {
	it := s.Items[i]
	clean, size, err := checkPaste(s.Items, i, it.Name, text)
	if err != nil {
		return err
	}
	if strings.TrimSpace(clean) == "" {
		return naErr{msg: it.Name + " is empty after the edit: x removes it"}
	}
	old := pastePlaceholder(it)
	it.Text, it.Bytes = clean, size
	s.Task.ReplaceFirst(old, pastePlaceholder(it))
	s.Items[i] = it
	return nil
}

// removeItem drops item i, and a paste's placeholder from the task.
func removeItem(s *NewAgentState, i int) {
	if i < 0 || i >= len(s.Items) {
		return
	}
	if it := s.Items[i]; it.Kind == spawnctx.Paste {
		s.Task.ReplaceFirst(pastePlaceholder(it), "")
	}
	s.Items = slices.Delete(s.Items, i, i+1)
	s.ItemIdx = min(s.ItemIdx, max(len(s.Items)-1, 0))
}

// naNameTaken reports whether an item other than index skip has this name.
func naNameTaken(items []spawnctx.Item, skip int, name string) bool {
	for j, o := range items {
		if j != skip && o.Name == name {
			return true
		}
	}
	return false
}

// naLines is "1 line" or "N lines".
func naLines(n int) string {
	if n == 1 {
		return "1 line"
	}
	return strconv.Itoa(n) + " lines"
}

// renameItem names paste i, and its placeholder in the task, name. A name
// that would break the prompt (spawnctx.CheckName: control characters,
// U+2028/U+2029, bidi or invisible runes, quotes) or that another item has
// is refused.
func renameItem(s *NewAgentState, i int, name string) error {
	if i < 0 || i >= len(s.Items) {
		return naErr{msg: "no item to rename"}
	}
	it := s.Items[i]
	if it.Kind != spawnctx.Paste {
		return naErr{msg: "only a paste can be renamed; " + it.Kind.String() + " items are named by their path"}
	}
	name = strings.TrimSpace(name)
	if err := spawnctx.CheckName(it.Kind, name); err != nil {
		return naErr{msg: "that name cannot go into the prompt: " + strings.TrimSuffix(err.Error(), ": "+spawnctx.ErrBadItem.Error())}
	}
	if naNameTaken(s.Items, i, name) {
		return naErr{msg: strconv.Quote(name) + " is taken by another item"}
	}
	old := pastePlaceholder(it)
	it.Name = name
	s.Task.ReplaceFirst(old, pastePlaceholder(it))
	s.Items[i] = it
	return nil
}

// naContextKey is a key in the Compose context list; ok false leaves it to
// the page (tab, digits, esc).
func naContextKey(m Model, k tea.KeyMsg) (Model, tea.Cmd, bool) {
	st := m.NewAgent
	if len(st.Items) == 0 {
		return m, nil, false
	}
	switch {
	case k.Type == tea.KeyDown || isRune(k, 'j'):
		st.ItemIdx = min(st.ItemIdx+1, len(st.Items)-1)
	case k.Type == tea.KeyUp || isRune(k, 'k'):
		st.ItemIdx = max(st.ItemIdx-1, 0)
	case k.Type == tea.KeyEnter:
		st.Screen, st.PrevScroll = naPreview, 0
		playNav(m)
	case k.Type == tea.KeyDelete || isRune(k, 'x'):
		removeItem(st, st.ItemIdx)
	case isRune(k, 'r'):
		naStartRename(st)
	default:
		return m, nil, false
	}
	return m, nil, true
}

// isRune reports a plain key typing r.
func isRune(k tea.KeyMsg, r rune) bool {
	return k.Type == tea.KeyRunes && !k.Alt && len(k.Runes) == 1 && k.Runes[0] == r
}

// naStartRename starts typing a new name for the item under the cursor.
func naStartRename(st *NewAgentState) {
	if st.ItemIdx >= len(st.Items) {
		return
	}
	if it := st.Items[st.ItemIdx]; it.Kind != spawnctx.Paste {
		st.Err = naFail(renameItem(st, st.ItemIdx, it.Name))
		return
	}
	st.Renaming = true
	st.RenameBuf = []rune(st.Items[st.ItemIdx].Name)
}

// naRenameKey is a key while a name is being typed: enter saves (a refused
// name stays in the editor with the error), esc cancels.
func naRenameKey(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	st := m.NewAgent
	switch k.Type {
	case tea.KeyEsc:
		st.Renaming, st.RenameBuf = false, nil
	case tea.KeyEnter:
		if err := renameItem(st, st.ItemIdx, string(st.RenameBuf)); err != nil {
			st.Err = naFail(err)
			return m, nil
		}
		st.Renaming, st.RenameBuf = false, nil
	case tea.KeyBackspace:
		if n := len(st.RenameBuf); n > 0 {
			st.RenameBuf = st.RenameBuf[:n-1]
		}
	case tea.KeyCtrlU:
		st.RenameBuf = nil
	case tea.KeyCtrlW:
		st.RenameBuf = []rune(strings.TrimRightFunc(strings.TrimRight(string(st.RenameBuf), " "), func(r rune) bool { return r != ' ' }))
	case tea.KeySpace:
		st.RenameBuf = appendCapped(st.RenameBuf, []rune{' '})
	case tea.KeyRunes:
		if !k.Alt {
			// Typed as it is: CheckName refuses what does not belong on
			// enter, so nothing is hidden by silently dropping it here.
			st.RenameBuf = appendCapped(st.RenameBuf, k.Runes)
		}
	}
	return m, nil
}

// appendCapped appends rs to buf up to naRenameMax runes.
func appendCapped(buf, rs []rune) []rune {
	return append(buf, rs[:min(len(rs), max(naRenameMax-len(buf), 0))]...)
}

// editorDoneMsg ends an $EDITOR run: Item is the paste edited, or -1 for
// the task; Text is the file as the editor left it.
type editorDoneMsg struct {
	Item int
	Text string
	Err  error
}

// execProcess hands the terminal to a child (tea.ExecProcess); tests swap it.
var execProcess = tea.ExecProcess

// editorCmd is the editor for path: $EDITOR split on white space and run
// directly, never through a shell, else vi (notepad on Windows). Tests swap
// it.
var editorCmd = func(path string) *exec.Cmd {
	args := strings.Fields(os.Getenv("EDITOR"))
	if len(args) == 0 {
		args = []string{"vi"}
		if runtime.GOOS == "windows" {
			args = []string{"notepad"}
		}
	}
	return exec.Command(args[0], append(args[1:], path)...) //nolint:gosec,noctx // G204: $EDITOR is the user's own program; noctx: it runs until the user quits it.
}

// editInEditor runs $EDITOR on text (paste item, or -1 for the task): the
// text goes to a 0600 file in a fresh 0700 temp folder, the TUI hands the
// terminal over, and the folder is removed as soon as the editor exits,
// whatever happened. Its editorDoneMsg carries the edited text.
func editInEditor(item int, text string) tea.Cmd {
	fail := func(err error) tea.Cmd {
		return func() tea.Msg { return editorDoneMsg{Item: item, Err: err} }
	}
	dir, err := os.MkdirTemp("", "rex-context-")
	if err != nil {
		return fail(err)
	}
	path := filepath.Join(dir, "context.txt")
	if err := writePrivate(path, text); err != nil {
		_ = os.RemoveAll(dir)
		return fail(err)
	}
	return execProcess(editorCmd(path), func(err error) tea.Msg {
		defer func() { _ = os.RemoveAll(dir) }()
		if err != nil {
			return editorDoneMsg{Item: item, Err: err}
		}
		b, err := readCapped(path, naEditorMax)
		return editorDoneMsg{Item: item, Text: string(b), Err: err}
	})
}

// writePrivate creates path, which must not exist, 0600, with text.
func writePrivate(path, text string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(text); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// readCapped reads path, refusing more than limit bytes.
func readCapped(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, naErr{msg: "the edited text is over " + naBytes(int(limit))}
	}
	return b, nil
}

// handleEditorDone takes the text back from $EDITOR into the task or the
// paste it came from. A failed editor or a refused text keeps what was
// there and says why.
func handleEditorDone(m Model, msg editorDoneMsg) (Model, tea.Cmd) {
	m.mouseOn = false // tea.ExecProcess turned mouse reporting off
	m, mc := syncMouse(m, "exec-return")
	st := m.NewAgent
	if st == nil {
		return m, mc
	}
	st.Rev++
	if msg.Err != nil {
		st.Err = "✗ editor: " + msg.Err.Error()
		return m, mc
	}
	switch {
	case msg.Item < 0:
		if spawnctx.IsBinary(msg.Text) {
			st.Err = naFail(naErr{msg: "the edited task looks like binary data, not text: it was not kept"})
			return m, mc
		}
		st.Task.Clear()
		st.Task.Insert(strings.TrimRight(spawnctx.Sanitize(msg.Text), "\n"))
	case msg.Item < len(st.Items) && st.Items[msg.Item].Kind == spawnctx.Paste:
		if err := setPaste(st, msg.Item, msg.Text); err != nil {
			st.Err = naFail(err)
		}
	}
	return m, mc
}

// naBytes is a size in the page's words: "312 B", "2.1 KB", "210 KB",
// "1.5 MB" (1 KB = 1024 bytes, as the caps).
func naBytes(n int) string {
	switch {
	case n < 1<<10:
		return strconv.Itoa(n) + " B"
	case n < 10<<10:
		return strconv.FormatFloat(float64(n)/(1<<10), 'f', 1, 64) + " KB"
	case n < 1<<20:
		return strconv.Itoa((n+512)>>10) + " KB"
	}
	return strconv.FormatFloat(float64(n)/(1<<20), 'f', 1, 64) + " MB"
}
