package export

import (
	"bufio"
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
	root := filepath.Join(home, ".claude", "projects")
	var found string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
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
	Type  string                     `json:"type"`
	Text  string                     `json:"text"`
	Name  string                     `json:"name"`
	Input map[string]json.RawMessage `json:"input"`
}

func parseAgentLog(r io.Reader) ([]Turn, error) {
	br := bufio.NewReaderSize(r, 1<<20)
	var turns []Turn
	for n := 1; ; n++ {
		line, err := br.ReadBytes('\n')
		if len(line) > maxLogLine {
			return nil, fmt.Errorf("export: agent log line %d is too large", n)
		}
		complete := err == nil
		if rec := strings.TrimSpace(string(line)); rec != "" {
			turn, ok, perr := parseLogLine([]byte(rec))
			switch {
			case perr != nil && !complete:
				// An unfinished last line: the agent is still writing it.
			case perr != nil:
				return nil, fmt.Errorf("export: agent log line %d: %w", n, perr)
			case ok:
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
	var text string
	if json.Unmarshal(l.Message.Content, &text) == nil {
		parts = append(parts, text)
	} else {
		var blocks []logBlock
		if err := json.Unmarshal(l.Message.Content, &blocks); err != nil {
			return Turn{}, false, nil
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
	for _, k := range toolArgKeys {
		raw, ok := b.Input[k]
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
