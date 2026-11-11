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
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
	"github.com/hinshun/vt10x"
)

// MaxTranscriptBytes caps what Strip reads, so a runaway transcript is an
// error and not an out-of-memory kill.
const MaxTranscriptBytes = 512 << 20

// ErrTooLarge reports a transcript above MaxTranscriptBytes.
var ErrTooLarge = errors.New("transcript is too large to export")

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
	if cols <= 0 || rows <= 0 {
		return nil, fmt.Errorf("export: screen size %dx%d: must be positive", cols, rows)
	}
	e := &extractor{cuts: slices.Sorted(slices.Values(cuts))}
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
	return e.frames, nil
}

// extractor is the state of one Frames call.
type extractor struct {
	screen vt10x.Terminal
	cuts   []int64  // turn cuts not yet reached, ascending
	scroll []string // lines scrolled off the screen since the last cut
	frames []Frame
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
	e.scroll = append(e.scroll, strings.TrimRight(b.String(), " "))
}

// process feeds data (whose first byte is at transcript offset base) to the
// screen, cutting a frame at every cut point inside it.
func (e *extractor) process(data []byte, base int64) error {
	end := base + int64(len(data))
	points := screenCuts(data)
	for len(e.cuts) > 0 && e.cuts[0] < end {
		points = append(points, int(max(e.cuts[0]-base, 0)))
		e.cuts = e.cuts[1:]
	}
	slices.Sort(points)
	points = slices.Compact(points)
	prev := 0
	for _, p := range points {
		if err := e.feed(data[prev:p]); err != nil {
			return err
		}
		e.emit(base + int64(p))
		prev = p
	}
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
	if text == "" || (len(e.frames) > 0 && e.frames[len(e.frames)-1].Text == text) {
		return
	}
	e.frames = append(e.frames, Frame{At: at, Text: text})
}

// snapshot is the scrolled-off lines plus the screen rows, trailing spaces
// trimmed and surrounding blank lines dropped.
func (e *extractor) snapshot() string {
	lines := slices.Clone(e.scroll)
	for row := range strings.SplitSeq(e.screen.String(), "\n") {
		lines = append(lines, strings.TrimRight(strings.ReplaceAll(row, "\x00", " "), " "))
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
	s := ansi.Strip(strings.ToValidUTF8(string(b), "�"))
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(overwrite(line), " \t")
	}
	return joinTrimmed(lines), nil
}

// overwrite resolves carriage returns in one line: each segment after a CR
// overwrites the start of what came before. Other controls are dropped.
func overwrite(line string) string {
	var cur []rune
	for seg := range strings.SplitSeq(line, "\r") {
		var next []rune
		for _, r := range seg {
			if r == '\t' || !unicode.IsControl(r) {
				next = append(next, r)
			}
		}
		if len(next) >= len(cur) {
			cur = next
			continue
		}
		copy(cur, next)
	}
	return string(cur)
}
