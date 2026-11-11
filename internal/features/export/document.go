package export

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"gopkg.in/yaml.v3"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// Version is the document version written as "version" (P4 versioning rule:
// a breaking change to the shape bumps it).
const Version = 1

// MaxOutputBytes caps a rendered document. Above it Render fails and the
// caller points at --max-frames.
const MaxOutputBytes = 20 << 20

// ErrOutputTooLarge reports a document above MaxOutputBytes.
var ErrOutputTooLarge = errors.New("export is larger than 20 MiB")

// Where the text of a document came from.
const (
	// SourceFrames is the transcript, replayed or stripped.
	SourceFrames = "frames"
	// SourceAgentLog is the agent's own conversation log (--from-agent-log).
	SourceAgentLog = "agent_log"
)

// Turn is one message of the agent's own log.
type Turn struct {
	Role string `json:"role"`
	Text string `json:"text"`
	// At is the message time as the log wrote it (RFC 3339); empty when it
	// had none.
	At string `json:"at,omitempty"`
}

// Document is one exported session. Exactly one of Frames and Turns is set.
type Document struct {
	Version int                     `json:"version"`
	Session protocol.SessionSummary `json:"session"`
	// Source is SourceFrames or SourceAgentLog.
	Source string `json:"source"`
	// Notice says why the export is not what was asked for (for example the
	// agent log was missing and frames were used instead).
	Notice string `json:"notice,omitempty"`
	// FramesDropped counts older frames cut by --max-frames.
	FramesDropped int `json:"frames_dropped,omitempty"`
	// TurnsDropped counts older turns cut by --max-frames.
	TurnsDropped int     `json:"turns_dropped,omitempty"`
	Frames       []Frame `json:"frames,omitempty"`
	Turns        []Turn  `json:"turns,omitempty"`
}

// Format is an output format.
type Format string

// The output formats. JSON is the default.
const (
	FormatJSON Format = "json"
	FormatYAML Format = "yaml"
	FormatMD   Format = "md"
	FormatTXT  Format = "txt"
)

// Formats lists the formats, default first.
func Formats() []Format { return []Format{FormatJSON, FormatYAML, FormatMD, FormatTXT} }

// Ext is the file extension (without the dot) the TUI gives the format.
func (f Format) Ext() string { return string(f) }

// ParseFormat parses a -o value.
func ParseFormat(s string) (Format, error) {
	for _, f := range Formats() {
		if string(f) == s {
			return f, nil
		}
	}
	return "", fmt.Errorf("unknown export format %q (use json, yaml, md or txt)", s)
}

// InferFormat picks a format from a file name's extension.
func InferFormat(path string) (Format, bool) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		return FormatJSON, true
	case ".yaml", ".yml":
		return FormatYAML, true
	case ".md", ".markdown":
		return FormatMD, true
	case ".txt":
		return FormatTXT, true
	}
	return "", false
}

// Render writes d in format f. It fails with ErrOutputTooLarge above
// MaxOutputBytes.
func Render(d Document, f Format) ([]byte, error) {
	var out []byte
	var err error
	switch f {
	case FormatJSON:
		out, err = renderJSON(d)
	case FormatYAML:
		out, err = renderYAML(d)
	case FormatMD:
		out = []byte(renderMD(d))
	case FormatTXT:
		out = []byte(renderTXT(d))
	default:
		return nil, fmt.Errorf("export: unknown format %q", f)
	}
	if err != nil {
		return nil, err
	}
	if len(out) > MaxOutputBytes {
		return nil, ErrOutputTooLarge
	}
	return out, nil
}

func renderJSON(d Document) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(d); err != nil {
		return nil, fmt.Errorf("export: encode json: %w", err)
	}
	return b.Bytes(), nil
}

// renderYAML writes the same document as renderJSON: the JSON, walked
// token by token into a yaml.Node tree (keys in order) and emitted in block
// style. It never parses the JSON as YAML, so text YAML would reject (DEL,
// C1, noncharacters) is quoted by the encoder instead.
func renderYAML(d Document) ([]byte, error) {
	j, err := json.Marshal(d)
	if err != nil {
		return nil, fmt.Errorf("export: encode yaml: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(j))
	dec.UseNumber()
	n, err := nodeFromJSON(dec)
	if err != nil {
		return nil, fmt.Errorf("export: encode yaml: %w", err)
	}
	var b bytes.Buffer
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(n); err != nil {
		return nil, fmt.Errorf("export: encode yaml: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("export: encode yaml: %w", err)
	}
	return b.Bytes(), nil
}

// nodeFromJSON reads one JSON value from dec as a yaml.Node.
func nodeFromJSON(dec *json.Decoder) (*yaml.Node, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch v := tok.(type) {
	case json.Delim:
		if v == '{' {
			n := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			for dec.More() {
				key, err := dec.Token()
				if err != nil {
					return nil, err
				}
				val, err := nodeFromJSON(dec)
				if err != nil {
					return nil, err
				}
				n.Content = append(n.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key.(string)}, val)
			}
			_, err := dec.Token()
			return n, err
		}
		n := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for dec.More() {
			val, err := nodeFromJSON(dec)
			if err != nil {
				return nil, err
			}
			n.Content = append(n.Content, val)
		}
		_, err := dec.Token()
		return n, err
	case string:
		n := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v}
		if strings.Contains(v, "\n") {
			n.Style = yaml.LiteralStyle
		}
		return n, nil
	case json.Number:
		tag := "!!int"
		if strings.ContainsAny(v.String(), ".eE") {
			tag = "!!float"
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: v.String()}, nil
	case bool:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(v)}, nil
	}
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}, nil
}

// metaRow is one metadata line of the md and txt documents.
type metaRow struct{ label, value string }

// metaRows is the session metadata shown in md and txt, empty values left
// out. Every value is one line of plain text.
func metaRows(d Document) []metaRow {
	s := d.Session
	rows := []metaRow{
		{"Tool", s.ToolID},
		{"Model", s.ModelID},
		{"Effort", s.Effort},
		{"Name", s.Name},
		{"Slug", s.Slug},
		{"ID", s.ID},
		{"Working directory", s.CWD},
		{"State", string(s.State)},
		{"Started", timeText(s.StartedAt)},
		{"Ended", timeText(s.EndedAt)},
		{"Resume ID", s.ResumeID},
		{"Description", s.Description},
		{"Source", d.Source},
	}
	out := rows[:0]
	for _, r := range rows {
		if r.value = oneLine(r.value); r.value != "" {
			out = append(out, r)
		}
	}
	return out
}

func timeText(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// oneLine makes untrusted text one line of plain text: escape sequences
// gone, control characters and line breaks turned into spaces.
func oneLine(s string) string {
	return strings.TrimSpace(protocol.FlattenDisplayText(ansi.Strip(s)))
}

func title(d Document) string {
	for _, s := range []string{d.Session.Name, d.Session.Slug, d.Session.ShortID} {
		if s = oneLine(s); s != "" {
			return s
		}
	}
	return "session"
}

func renderMD(d Document) string {
	var b strings.Builder
	b.WriteString("# Session export: " + title(d) + "\n\n")
	b.WriteString("| Field | Value |\n| --- | --- |\n")
	for _, r := range metaRows(d) {
		b.WriteString("| " + r.label + " | " + strings.ReplaceAll(r.value, "|", `\|`) + " |\n")
	}
	if n := oneLine(d.Notice); n != "" {
		b.WriteString("\n> " + n + "\n")
	}
	if d.FramesDropped > 0 {
		b.WriteString("\n_" + strconv.Itoa(d.FramesDropped) + " earlier frames left out (--max-frames)._\n")
	}
	if d.TurnsDropped > 0 {
		b.WriteString("\n_" + strconv.Itoa(d.TurnsDropped) + " earlier turns left out (--max-frames)._\n")
	}
	for i, f := range d.Frames {
		b.WriteString("\n## Frame " + strconv.Itoa(i+1) + "\n\n")
		writeFenced(&b, f.Text)
	}
	for i, t := range d.Turns {
		head := "## Turn " + strconv.Itoa(i+1) + " · " + oneLine(t.Role)
		if at := oneLine(t.At); at != "" {
			head += " · " + at
		}
		b.WriteString("\n" + head + "\n\n")
		writeFenced(&b, t.Text)
	}
	return b.String()
}

// writeFenced writes text as a fenced block, with a fence longer than any
// run of backticks inside it.
func writeFenced(b *strings.Builder, text string) {
	run, longest := 0, 0
	for _, r := range text {
		if r == '`' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	fence := strings.Repeat("`", max(3, longest+1))
	b.WriteString(fence + "text\n" + text + "\n" + fence + "\n")
}

// rule separates the frames and turns of the txt document.
var rule = strings.Repeat("-", 72)

func renderTXT(d Document) string {
	var b strings.Builder
	b.WriteString("rex session export: " + title(d) + "\n")
	for _, r := range metaRows(d) {
		b.WriteString(strings.ToLower(r.label) + ": " + r.value + "\n")
	}
	if n := oneLine(d.Notice); n != "" {
		b.WriteString("notice: " + n + "\n")
	}
	if d.FramesDropped > 0 {
		b.WriteString("frames left out: " + strconv.Itoa(d.FramesDropped) + " (--max-frames)\n")
	}
	if d.TurnsDropped > 0 {
		b.WriteString("turns left out: " + strconv.Itoa(d.TurnsDropped) + " (--max-frames)\n")
	}
	for _, f := range d.Frames {
		b.WriteString("\n" + rule + "\n" + f.Text + "\n")
	}
	for _, t := range d.Turns {
		head := "[" + oneLine(t.Role) + "]"
		if at := oneLine(t.At); at != "" {
			head += " " + at
		}
		b.WriteString("\n" + rule + "\n" + head + "\n" + t.Text + "\n")
	}
	return b.String()
}
