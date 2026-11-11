package core

import (
	"io"
	"strings"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// Result is what a command hands to Printer.Emit: the data once, in the
// shapes every format needs.
type Result struct {
	// Cmd is the command name, used in json and agent envelopes. Empty means
	// the Printer's current command.
	Cmd string
	// Data is the json payload: a typed struct with snake_case tags, never
	// display strings. Sessions reuse protocol.SessionSummary.
	Data any
	// Meta qualifies the data (for example "source": "disk"). It appears as
	// "meta" in json and as trailer fields in agent mode, sorted by key.
	Meta map[string]any
	// View is the human and agent layout.
	View View
	// Legacy writes the command's pre-versioned -json output. It is required
	// for commands registered with WithLegacyJSON.
	Legacy func(io.Writer) error
}

// View is an ordered list of blocks.
type View []Block

// Block is one element of a View. The kit's block types are Header, Section,
// KV, Table, Checks, Note, Hint and Line.
type Block interface{ isBlock() }

// Cell is one value in a Table or KV.
type Cell struct {
	// Text is the human rendering (text and md).
	Text string
	// Txt overrides Text in the txt format when non-empty, so a view can keep
	// a stable machine-friendly spelling (for example the raw state name).
	Txt string
	// Raw is the machine value for agent lines: a string, bool, integer or
	// time.Time. Nil means Text.
	Raw any
	// Tone colors the cell in the text format.
	Tone Tone
}

// Align is a column's text alignment.
type Align uint8

// Alignments.
const (
	AlignLeft Align = iota
	AlignRight
)

// Col describes one Table column.
type Col struct {
	// Key names the column in agent lines (lowercase snake_case).
	Key string
	// Title is the header text; renderers uppercase it.
	Title string
	Align Align
	// Flex columns shrink, with an ellipsis, when the text format is wider
	// than the terminal.
	Flex bool
	// Min is the narrowest a Flex column shrinks to (default 6).
	Min int
	// Max truncates cells to this many display cells in text and txt
	// (0 = no limit).
	Max int
	// Width pads the column to a fixed width in txt (0 = widest cell). The
	// last column is never padded.
	Width int
	// DropBelow hides the column in the text format when the terminal is
	// narrower than this many cells (0 = never), so low-value columns give
	// way before the others are squeezed.
	DropBelow int
	// Formats limits the column to these formats; zero means all. Rows still
	// carry a cell for every column.
	Formats FormatSet
}

func (c Col) shownIn(f Format) bool { return c.Formats == 0 || c.Formats.Has(f) }

// Header is a one-line title with dim meta text.
type Header struct{ Title, Meta string }

// Section is a Title-case heading with a trailing colon at column 0.
type Section struct{ Title string }

// Pair is one KV entry.
type Pair struct {
	// Key names the field in agent lines (lowercase snake_case).
	Key string
	// Label is the human label.
	Label string
	Val   Cell
	// Glyph is shown before the value in an inline KV (a state marker).
	Glyph string
	// Formats limits the pair to these formats; zero means all.
	Formats FormatSet
}

// pairs returns the pairs shown in f.
func (k KV) pairs(f Format) []Pair {
	var out []Pair
	for _, p := range k.Pairs {
		if p.Formats == 0 || p.Formats.Has(f) {
			out = append(out, p)
		}
	}
	return out
}

// KV is a list of labeled values.
//
// In agent mode a KV with Kind set is one record line; a KV with an empty
// Kind contributes its pairs to the closing "ok" line.
type KV struct {
	Kind  string
	Pairs []Pair
	// Inline renders the pairs on one line in the text format:
	// "◆ 1 needs you   ⠋ 1 working".
	Inline bool
	// Txt replaces the whole block in the txt format when non-empty (for an
	// inline summary whose plain spelling is a stable contract).
	Txt string
}

// Table is rows under a header. In agent mode a Table with Kind set is one
// record line per row.
type Table struct {
	Kind string
	Cols []Col
	Rows [][]Cell
	// Empty is shown instead of the table when there are no rows.
	Empty string
}

// Check is one doctor-style row.
type Check struct {
	Name string
	// Status is "pass", "warn", "fail" or "info".
	Status string
	Detail string
	Hint   string
}

// Checks is a list of status rows.
type Checks struct{ Rows []Check }

// Note is one dim line. ToneNone, ToneDim and ToneMuted read as plain notes;
// ToneWarn and ToneOK as a warning or a success.
type Note struct {
	Tone Tone
	Text string
}

// Hint is a closing "next command" line. A view has at most one. It is not
// rendered in txt, json or agent.
type Hint struct{ Text, Cmd string }

// Line is a one-line confirmation, for example "✓ started 7d4f fix-auth".
type Line struct {
	Tone  Tone
	Glyph string
	Text  string
	// Txt replaces the line in the txt format when non-empty (a tab-separated
	// record, say).
	Txt string
}

func (Header) isBlock()  {}
func (Section) isBlock() {}
func (KV) isBlock()      {}
func (Table) isBlock()   {}
func (Checks) isBlock()  {}
func (Note) isBlock()    {}
func (Hint) isBlock()    {}
func (Line) isBlock()    {}

// flat strips control, line-break and bidi characters from untrusted text.
func flat(s string) string { return protocol.FlattenDisplayText(s) }

// flatKeepTab is flat for fields that may hold tab-separated records.
func flatKeepTab(s string) string {
	if !strings.Contains(s, "\t") {
		return flat(s)
	}
	parts := strings.Split(s, "\t")
	for i, p := range parts {
		parts[i] = flat(p)
	}
	return strings.Join(parts, "\t")
}

// sanitized returns a copy of v with every string flattened. The human
// formats render the copy, so a command cannot forget to flatten a session
// slug or a daemon message.
func (v View) sanitized() View {
	out := make(View, len(v))
	for i, b := range v {
		switch b := b.(type) {
		case Header:
			out[i] = Header{Title: flat(b.Title), Meta: flat(b.Meta)}
		case Section:
			out[i] = Section{Title: flat(b.Title)}
		case KV:
			pairs := make([]Pair, len(b.Pairs))
			for j, p := range b.Pairs {
				p.Label, p.Glyph, p.Val = flat(p.Label), flat(p.Glyph), flatCell(p.Val)
				pairs[j] = p
			}
			b.Pairs, b.Txt = pairs, flatKeepTab(b.Txt)
			out[i] = b
		case Table:
			rows := make([][]Cell, len(b.Rows))
			for j, r := range b.Rows {
				cells := make([]Cell, len(r))
				for k, c := range r {
					cells[k] = flatCell(c)
				}
				rows[j] = cells
			}
			b.Rows, b.Empty = rows, flat(b.Empty)
			out[i] = b
		case Checks:
			rows := make([]Check, len(b.Rows))
			for j, c := range b.Rows {
				rows[j] = Check{Name: flat(c.Name), Status: c.Status, Detail: flat(c.Detail), Hint: flat(c.Hint)}
			}
			out[i] = Checks{Rows: rows}
		case Note:
			out[i] = Note{Tone: b.Tone, Text: flat(b.Text)}
		case Hint:
			out[i] = Hint{Text: flat(b.Text), Cmd: flat(b.Cmd)}
		case Line:
			out[i] = Line{Tone: b.Tone, Glyph: flat(b.Glyph), Text: flat(b.Text), Txt: flatKeepTab(b.Txt)}
		default:
			out[i] = b
		}
	}
	return out
}

func flatCell(c Cell) Cell {
	c.Text, c.Txt = flat(c.Text), flatKeepTab(c.Txt)
	return c
}

func (c Cell) txt() string {
	if c.Txt != "" {
		return c.Txt
	}
	return c.Text
}
