package export

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// ErrNoAgentLog reports that the agent's own log for a conversation is not
// on disk.
var ErrNoAgentLog = errors.New("agent log not found")

// maxTurns caps the messages read from one agent log.
const maxTurns = 200000

// maxLogLine caps one line of the agent log (tool results can be large).
const maxLogLine = 64 << 20

// maxToolArgRunes caps the argument shown in a tool call's one-line summary.
const maxToolArgRunes = 100

// toolArgKeys are the tool input fields shown in a tool call's summary, in
// order of preference.
var toolArgKeys = []string{"file_path", "path", "command", "pattern", "url", "query", "description"}

// ReadAgentLog reads Claude Code's own log for the conversation resumeID:
// the file <home>/.claude/projects/<project>/<resumeID>.jsonl. It is opened
// read-only and never written. Only roles and message text are kept; each
// tool call becomes one "[tool] Name argument" line; thinking, tool results,
// meta messages and subagent (sidechain) messages are left out. A missing
// file is ErrNoAgentLog. A line that is not JSON is an error, except an
// unfinished last line (the agent may be writing it right now).
func ReadAgentLog(home, resumeID string) ([]Turn, error) {
	if !protocol.ValidResumeID(resumeID) {
		return nil, fmt.Errorf("export: resume id %q is not a UUID", resumeID)
	}
	path, err := findAgentLog(home, resumeID)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("export: %w", err)
	}
	defer f.Close()
	return parseAgentLog(f)
}

// findAgentLog locates <resumeID>.jsonl one or two folders below
// <home>/.claude/projects, as a regular file.
func findAgentLog(home, resumeID string) (string, error) {
	root, err := filepath.EvalSymlinks(filepath.Join(home, ".claude", "projects"))
	if errors.Is(err, fs.ErrNotExist) {
		return "", ErrNoAgentLog
	}
	if err != nil {
		return "", fmt.Errorf("export: %w", err)
	}
	var found string
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p != root && d != nil && d.IsDir() {
				return fs.SkipDir // an unreadable project folder is not the one
			}
			return err
		}
		rel, _ := filepath.Rel(root, p)
		depth := strings.Count(rel, string(filepath.Separator)) + 1
		switch {
		case d.IsDir():
			if depth > 2 {
				return fs.SkipDir
			}
		case d.Type().IsRegular() && d.Name() == resumeID+".jsonl":
			found = p
			return fs.SkipAll
		}
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) || (err == nil && found == "") {
		return "", ErrNoAgentLog
	}
	if err != nil {
		return "", fmt.Errorf("export: %w", err)
	}
	return found, nil
}

// logLine is the part of one JSONL record the export uses.
type logLine struct {
	Type        string `json:"type"`
	IsMeta      bool   `json:"isMeta"`
	IsSidechain bool   `json:"isSidechain"`
	Timestamp   string `json:"timestamp"`
	Message     struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// logBlock is one element of an array message content.
type logBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

func parseAgentLog(r io.Reader) ([]Turn, error) {
	br := bufio.NewReaderSize(r, 1<<20)
	var turns []Turn
	for n := 1; ; n++ {
		line, err := readLine(br, maxLogLine)
		complete := err == nil
		if rec := strings.TrimSpace(string(line)); rec != "" {
			turn, ok, perr := parseLogLine([]byte(rec))
			switch {
			case perr != nil && !complete:
				// An unfinished last line: the agent is still writing it.
			case perr != nil:
				return nil, fmt.Errorf("export: agent log line %d: %w", n, perr)
			case ok:
				if len(turns) >= maxTurns {
					return nil, fmt.Errorf("export: agent log has more than %d messages: %w", maxTurns, ErrTooLarge)
				}
				turns = append(turns, turn)
			}
		}
		if errors.Is(err, io.EOF) {
			return turns, nil
		}
		if err != nil {
			return nil, fmt.Errorf("export: read agent log: %w", err)
		}
	}
}

// readLine returns the next line of br including its newline. A line longer
// than limit is read through but not kept: the result is empty (a tool result
// that large is no conversation text). The error is nil after a full line.
func readLine(br *bufio.Reader, limit int) ([]byte, error) {
	var line []byte
	skipped := false
	for {
		chunk, err := br.ReadSlice('\n')
		if !skipped {
			if len(line)+len(chunk) > limit {
				skipped, line = true, nil
			} else {
				line = append(line, chunk...)
			}
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		return line, err
	}
}

// parseLogLine turns one record into a turn. ok is false for records that
// are not part of the conversation text.
func parseLogLine(rec []byte) (Turn, bool, error) {
	var l logLine
	if err := json.Unmarshal(rec, &l); err != nil {
		return Turn{}, false, err
	}
	if (l.Type != "user" && l.Type != "assistant") || l.IsMeta || l.IsSidechain {
		return Turn{}, false, nil
	}
	var parts []string
	switch content := bytes.TrimSpace(l.Message.Content); {
	case len(content) > 0 && content[0] == '"':
		var text string
		if err := json.Unmarshal(content, &text); err != nil {
			return Turn{}, false, err
		}
		parts = append(parts, text)
	case len(content) > 0 && content[0] == '[':
		var blocks []logBlock
		if err := json.Unmarshal(content, &blocks); err != nil {
			return Turn{}, false, err
		}
		for _, b := range blocks {
			switch b.Type {
			case "text":
				parts = append(parts, b.Text)
			case "tool_use":
				parts = append(parts, toolSummary(b))
			}
		}
	}
	var kept []string
	for _, p := range parts {
		if p = cleanText(p); p != "" {
			kept = append(kept, p)
		}
	}
	if len(kept) == 0 {
		return Turn{}, false, nil
	}
	return Turn{Role: l.Type, Text: strings.Join(kept, "\n"), At: oneLine(l.Timestamp)}, true, nil
}

// toolSummary is the one-line summary of a tool call.
func toolSummary(b logBlock) string {
	out := "[tool] " + oneLine(b.Name)
	var input map[string]json.RawMessage
	_ = json.Unmarshal(b.Input, &input) // not an object: no argument to show
	for _, k := range toolArgKeys {
		raw, ok := input[k]
		if !ok {
			continue
		}
		var s string
		if json.Unmarshal(raw, &s) != nil {
			continue
		}
		if s = oneLine(s); s == "" {
			continue
		}
		if r := []rune(s); len(r) > maxToolArgRunes {
			s = string(r[:maxToolArgRunes-1]) + "…"
		}
		return out + " " + s
	}
	return out
}

// cleanText removes escape sequences and control characters (keeping line
// breaks and tabs) and replaces invalid UTF-8.
func cleanText(s string) string {
	s = ansi.Strip(strings.ToValidUTF8(s, "�"))
	s = strings.Map(func(r rune) rune {
		if r != '\n' && r != '\t' && unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	return strings.TrimSpace(s)
}
