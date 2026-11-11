// Package export turns a session's raw PTY transcript into readable text:
// screen "frames" for full-screen agents (the transcript replayed through
// the vt10x emulator), an escape-stripped text for line-oriented tools, and
// the json, yaml, md and txt documents `rex export` writes. Nothing here
// uploads anything or touches a running session.
package export

import (
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
	"github.com/hinshun/vt10x"
)

// MaxTranscriptBytes caps what Strip reads, so a runaway transcript is an
// error and not an out-of-memory kill.
const MaxTranscriptBytes = 512 << 20

// ErrTooLarge reports a transcript above MaxTranscriptBytes.
var ErrTooLarge = errors.New("input is too large to export")

// readChunk is how much of the transcript is read at a time. Frames never
// holds more than one chunk plus the screen.
const readChunk = 64 << 10

// maxEscapeCarry is how far back from a chunk's end an unfinished escape
// sequence is looked for (and carried into the next chunk).
const maxEscapeCarry = 32

// Frame is the text of the screen at one cut point.
type Frame struct {
	// At is the byte offset in the transcript where the frame was cut.
	// The transcript has no timestamps.
	At int64 `json:"at" yaml:"at"`
	// Text is the screen: lines that scrolled off since the previous cut,
	// then the screen rows, trailing blanks trimmed.
	Text string `json:"text" yaml:"text"`
}

// lineTools are the tools whose output is plain lines: ANSI stripping gives
// a good log and a screen replay adds nothing. Every other tool, including
// one the user added to tools.yaml, is replayed through the emulator.
var lineTools = map[string]bool{"echo": true, "ollama": true}

// LineTool reports whether the tool prints plain lines (use Strip) and not a
// full-screen UI (use Frames).
func LineTool(toolID string) bool { return lineTools[toolID] }

// Frames replays the transcript in r through a cols x rows virtual screen
// and returns the screen text at each cut point, oldest first: before every
// clear-screen (CSI 2J, 3J) and alternate-screen switch (CSI ? 47, 1047 or
// 1049 h/l), at each byte offset in cuts (turn boundaries; pass nil when
// there are none), and at the end. Blank frames and frames equal to the
// previous one are dropped, so a redraw-heavy transcript collapses to its
// distinct screens.
//
// Invalid UTF-8 becomes U+FFFD. Operating system commands (clipboard,
// window title, hyperlinks, rex's private ones) and every other escape the
// emulator does not draw produce no text. The emulator keeps the size it was
// given: resizes during the session are not replayed.
func Frames(r io.Reader, cols, rows int, cuts []int64) ([]Frame, error) {
	frames, _, err := FramesLimited(r, cols, rows, cuts, 0)
	return frames, err
}

// scrollFrameLines is how many scrolled-off lines make a frame of their own,
// so a tool that never clears the screen still gives many frames.
const scrollFrameLines = 200

// FramesLimited is Frames keeping only the last limit frames (0 = all). It
// holds at most max frames while it reads, so memory is bounded; dropped
// counts the older frames left out.
func FramesLimited(r io.Reader, cols, rows int, cuts []int64, limit int) (frames []Frame, dropped int, err error) {
	e, err := extract(r, cols, rows, cuts, limit)
	if err != nil {
		return nil, 0, err
	}
	return e.frames, e.dropped, nil
}

func extract(r io.Reader, cols, rows int, cuts []int64, limit int) (*extractor, error) {
	if cols <= 0 || rows <= 0 {
		return nil, fmt.Errorf("export: screen size %dx%d: must be positive", cols, rows)
	}
	if limit < 0 {
		return nil, fmt.Errorf("export: max frames %d: must not be negative", limit)
	}
	e := &extractor{cuts: slices.Sorted(slices.Values(cuts)), max: limit}
	e.screen = vt10x.New(vt10x.WithSize(cols, rows), vt10x.WithScrollback(e.scrolled))

	buf := make([]byte, readChunk)
	var carry []byte
	var base int64 // transcript offset of carry[0]
	for {
		n, err := r.Read(buf)
		carry = append(carry, buf[:n]...)
		if keep := incompleteTail(carry); n > 0 && keep < len(carry) {
			done := len(carry) - keep
			if perr := e.process(carry[:done], base); perr != nil {
				return nil, perr
			}
			base += int64(done)
			carry = carry[:copy(carry, carry[done:])]
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("export: read transcript: %w", err)
		}
	}
	if err := e.process(carry, base); err != nil {
		return nil, err
	}
	e.emit(base + int64(len(carry)))
	e.compact()
	return e, nil
}

// extractor is the state of one Frames call.
type extractor struct {
	screen  vt10x.Terminal
	cuts    []int64  // turn cuts not yet reached, ascending
	scroll  []string // lines scrolled off the screen since the last cut
	frames  []Frame
	max     int // keep only the last max frames (0 = all)
	dropped int
	at      int64 // transcript offset of the data being fed
	peak    int   // most frames held at once
}

// scrolled receives each line the emulator scrolls off the top.
func (e *extractor) scrolled(line []vt10x.Glyph) {
	var b strings.Builder
	for _, g := range line {
		if g.Char == 0 {
			b.WriteByte(' ')
			continue
		}
		b.WriteRune(g.Char)
	}
	e.scroll = append(e.scroll, strings.TrimRight(dropUnsafe(b.String()), " "))
	if len(e.scroll) >= scrollFrameLines {
		// Called with the emulator locked: cut the lines alone, without the screen.
		e.add(Frame{At: e.at, Text: joinTrimmed(e.scroll)})
		e.scroll = e.scroll[:0]
	}
}

// add appends a frame unless it is blank or equal to the last, dropping the
// oldest beyond max.
func (e *extractor) add(f Frame) {
	if f.Text == "" || (len(e.frames) > 0 && e.frames[len(e.frames)-1].Text == f.Text) {
		return
	}
	e.frames = append(e.frames, f)
	e.peak = max(e.peak, len(e.frames))
	if e.max > 0 && len(e.frames) > e.max {
		e.dropped++
		if len(e.frames) > 2*e.max {
			e.compact()
		}
	}
}

// compact drops the frames beyond the last max, releasing their memory.
func (e *extractor) compact() {
	if e.max > 0 && len(e.frames) > e.max {
		n := copy(e.frames, e.frames[len(e.frames)-e.max:])
		clear(e.frames[n:])
		e.frames = e.frames[:n]
	}
}

// process feeds data (whose first byte is at transcript offset base) to the
// screen, cutting a frame at every cut point inside it.
func (e *extractor) process(data []byte, base int64) error {
	end := base + int64(len(data))
	points := screenCuts(data)
	for len(e.cuts) > 0 && e.cuts[0] < end {
		p := int(max(e.cuts[0]-base, 0))
		for p < len(data) && !utf8.RuneStart(data[p]) {
			p++ // never cut inside a rune
		}
		points = append(points, p)
		e.cuts = e.cuts[1:]
	}
	slices.Sort(points)
	points = slices.Compact(points)
	prev := 0
	for _, p := range points {
		e.at = base + int64(prev)
		if err := e.feed(data[prev:p]); err != nil {
			return err
		}
		e.emit(base + int64(p))
		prev = p
	}
	e.at = base + int64(prev)
	return e.feed(data[prev:])
}

// feed writes b to the screen as valid UTF-8.
func (e *extractor) feed(b []byte) error {
	if len(b) == 0 {
		return nil
	}
	if _, err := e.screen.Write([]byte(strings.ToValidUTF8(string(b), "�"))); err != nil {
		return fmt.Errorf("export: replay transcript: %w", err)
	}
	return nil
}

// emit cuts a frame at offset at, unless the screen is blank or unchanged
// since the last frame.
func (e *extractor) emit(at int64) {
	text := e.snapshot()
	e.scroll = e.scroll[:0]
	e.add(Frame{At: at, Text: text})
}

// snapshot is the scrolled-off lines plus the screen rows, trailing spaces
// trimmed and surrounding blank lines dropped.
func (e *extractor) snapshot() string {
	lines := slices.Clone(e.scroll)
	for row := range strings.SplitSeq(e.screen.String(), "\n") {
		lines = append(lines, strings.TrimRight(dropUnsafe(strings.ReplaceAll(row, "\x00", " ")), " "))
	}
	return joinTrimmed(lines)
}

// joinTrimmed joins lines with newlines after dropping blank lines at both
// ends.
func joinTrimmed(lines []string) string {
	start, end := 0, len(lines)
	for start < end && lines[start] == "" {
		start++
	}
	for end > start && lines[end-1] == "" {
		end--
	}
	return strings.Join(lines[start:end], "\n")
}

// screenCuts returns the indexes in data where a cut sequence starts:
// clear-screen and alternate-screen switches.
func screenCuts(data []byte) []int {
	var out []int
	for i := 0; i+1 < len(data); i++ {
		if data[i] != 0x1b || data[i+1] != '[' {
			continue
		}
		j := i + 2
		for j < len(data) && data[j] >= 0x20 && data[j] <= 0x3f {
			j++
		}
		if j < len(data) && data[j] >= 0x40 && data[j] <= 0x7e && isCutSequence(string(data[i+2:j]), data[j]) {
			out = append(out, i)
		}
		i = j - 1
	}
	return out
}

// isCutSequence reports whether CSI params final clears the screen or
// switches to or from the alternate screen.
func isCutSequence(params string, final byte) bool {
	switch final {
	case 'J':
		return params == "2" || params == "3"
	case 'h', 'l':
		rest, private := strings.CutPrefix(params, "?")
		if !private {
			return false
		}
		for p := range strings.SplitSeq(rest, ";") {
			if p == "47" || p == "1047" || p == "1049" {
				return true
			}
		}
	}
	return false
}

// incompleteTail returns how many bytes at the end of data are the start of
// a UTF-8 rune or a CSI sequence whose end has not been read yet.
func incompleteTail(data []byte) int {
	keep := 0
	for k := 1; k <= min(utf8.UTFMax-1, len(data)); k++ {
		b := data[len(data)-k]
		if !utf8.RuneStart(b) {
			continue
		}
		if b >= 0xc0 && !utf8.FullRune(data[len(data)-k:]) {
			keep = k
		}
		break
	}
	lo := max(len(data)-maxEscapeCarry, 0)
	if p := lo + strings.LastIndexByte(string(data[lo:]), 0x1b); p >= lo && p < len(data) {
		if p == len(data)-1 {
			return max(keep, 1)
		}
		if data[p+1] == '[' {
			j := p + 2
			for j < len(data) && data[j] >= 0x20 && data[j] <= 0x3f {
				j++
			}
			if j == len(data) {
				return max(keep, len(data)-p)
			}
		}
	}
	return keep
}

// Strip returns the text of a line-oriented transcript: escape sequences
// (including operating system commands) removed, invalid UTF-8 replaced by
// U+FFFD, other control characters dropped, a carriage return overwriting
// the line so far (a progress bar keeps its last state) and trailing blanks
// trimmed.
func Strip(r io.Reader) (string, error) {
	b, err := io.ReadAll(io.LimitReader(r, MaxTranscriptBytes+1))
	if err != nil {
		return "", fmt.Errorf("export: read transcript: %w", err)
	}
	if len(b) > MaxTranscriptBytes {
		return "", ErrTooLarge
	}
	s := ansi.Strip(markLineEdits(strings.ToValidUTF8(string(b), "�")))
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(overwrite(line), " \t")
	}
	return joinTrimmed(lines), nil
}

// Markers for the line edits Strip honours. They are private-use runes, so
// markLineEdits first removes any the transcript itself holds.
const (
	markEL0 = '' // erase from the cursor to the end of the line
	markEL1 = '' // erase from the start of the line to the cursor
	markEL2 = '' // erase the whole line
	markCHA = '' // move to a column: followed by markColBase+column
	// markColBase offsets the column that follows markCHA (plane 15 private use).
	markColBase = 0xF0000
)

var lineEditRe = regexp.MustCompile(`\x1b\[(\d*)([KG])`)

// markLineEdits replaces erase-in-line (CSI K) and cursor-column (CSI G)
// sequences with markers, which ansi.Strip leaves alone and overwrite acts on.
func markLineEdits(s string) string {
	s = strings.Map(func(r rune) rune {
		if (r >= markEL0 && r <= markCHA) || r >= markColBase {
			return -1
		}
		return r
	}, s)
	return lineEditRe.ReplaceAllStringFunc(s, func(m string) string {
		n, _ := strconv.Atoi(m[2 : len(m)-1])
		if m[len(m)-1] == 'G' {
			return string(markCHA) + string(rune(markColBase+min(n, 10000)))
		}
		switch n {
		case 1:
			return string(markEL1)
		case 2:
			return string(markEL2)
		}
		return string(markEL0)
	})
}

// overwrite resolves one line as a line editor: a carriage return or CSI G
// moves the cursor, text overwrites from there, CSI K erases. Other
// controls are dropped.
func overwrite(line string) string {
	var cells []rune
	col := 0
	runes := []rune(line)
	for i := 0; i < len(runes); i++ {
		switch r := runes[i]; {
		case r == '\r':
			col = 0
		case r == markEL0:
			cells = cells[:min(col, len(cells))]
		case r == markEL1:
			for j := 0; j < min(col+1, len(cells)); j++ {
				cells[j] = ' '
			}
		case r == markEL2:
			cells = cells[:0]
		case r == markCHA:
			if i+1 < len(runes) {
				i++
				col = max(int(runes[i])-markColBase-1, 0)
			}
		case r != '\t' && unsafeRune(r):
		default:
			for len(cells) < col {
				cells = append(cells, ' ')
			}
			if col < len(cells) {
				cells[col] = r
			} else {
				cells = append(cells, r)
			}
			col++
		}
	}
	return string(cells)
}
