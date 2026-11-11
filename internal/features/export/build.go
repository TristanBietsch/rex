package export

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"

	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// Screen size used to replay a transcript: the daemon's default PTY size.
// The transcript does not record the size the session ran at.
const (
	DefaultCols = 120
	DefaultRows = 32
)

// ErrNoTranscript reports a session with no transcript file (pruned, or
// never started).
var ErrNoTranscript = errors.New("no transcript")

// ErrExists reports an output file that exists and was not to be replaced.
var ErrExists = errors.New("file exists")

// Options say what to export.
type Options struct {
	Session protocol.SessionSummary
	// TranscriptPath is the session's transcript.log.
	TranscriptPath string
	// MaxFrames keeps only the last N frames when > 0.
	MaxFrames int
	// Cols and Rows size the replay screen; zero means DefaultCols x DefaultRows.
	Cols, Rows int
	// Cuts are extra frame cut points as transcript byte offsets (turn
	// boundaries from the turn ledger, when there is one).
	Cuts []int64
	// FromAgentLog reads Claude Code's own conversation log instead of the
	// transcript (see ReadAgentLog). When that is not possible the export
	// falls back to frames and says why in Document.Notice.
	FromAgentLog bool
	// Home is the home directory the agent log is looked for under; empty
	// means the current user's.
	Home string
}

// Build reads the session's transcript and returns the document. The file
// is opened read-only and read up to the size it has at that moment, so a
// running session keeps writing undisturbed and bytes appended meanwhile are
// left for the next export. Full-screen tools are replayed (Frames), line
// tools stripped (Strip).
func Build(o Options) (Document, error) {
	if o.MaxFrames < 0 {
		return Document{}, fmt.Errorf("export: max frames %d: must not be negative", o.MaxFrames)
	}
	d := Document{Version: Version, Session: o.Session, Source: SourceFrames}
	if o.FromAgentLog {
		turns, notice, err := agentTurns(o)
		if err != nil {
			return d, err
		}
		if notice == "" {
			d.Source, d.Turns = SourceAgentLog, turns
			return d, nil
		}
		d.Notice = notice
	}
	f, err := os.Open(o.TranscriptPath)
	if errors.Is(err, fs.ErrNotExist) {
		return d, fmt.Errorf("%w for session %s", ErrNoTranscript, o.Session.ShortID)
	}
	if err != nil {
		return d, fmt.Errorf("export: %w", err)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return d, fmt.Errorf("export: %w", err)
	}
	r := io.LimitReader(f, st.Size())

	if LineTool(o.Session.ToolID) {
		text, err := Strip(r)
		if err != nil {
			return d, err
		}
		if text != "" {
			d.Frames = []Frame{{At: st.Size(), Text: text}}
		}
		return d, nil
	}
	cols, rows := o.Cols, o.Rows
	if cols == 0 {
		cols = DefaultCols
	}
	if rows == 0 {
		rows = DefaultRows
	}
	frames, err := Frames(r, cols, rows, o.Cuts)
	if err != nil {
		return d, err
	}
	if o.MaxFrames > 0 && len(frames) > o.MaxFrames {
		d.FramesDropped = len(frames) - o.MaxFrames
		frames = frames[d.FramesDropped:]
	}
	d.Frames = frames
	return d, nil
}

// agentTurns reads the agent log for o.Session. A non-empty notice means it
// could not be used and the export should show frames, for that reason.
func agentTurns(o Options) (turns []Turn, notice string, err error) {
	s := o.Session
	switch {
	case s.ToolID != "claude":
		return nil, fmt.Sprintf("--from-agent-log reads Claude Code logs only, not %s; showing frames", s.ToolID), nil
	case s.ResumeID == "":
		return nil, "session has no resume id, so its agent log cannot be found; showing frames", nil
	}
	home := o.Home
	if home == "" {
		if home, err = paths.Home(); err != nil {
			return nil, "", fmt.Errorf("export: %w", err)
		}
	}
	turns, err = ReadAgentLog(home, s.ResumeID)
	if errors.Is(err, ErrNoAgentLog) {
		return nil, "agent log for resume id " + s.ResumeID + " not found under ~/.claude/projects; showing frames", nil
	}
	return turns, "", err
}

// WriteFile writes data to path with mode 0600. Without force it refuses to
// replace a file (ErrExists); with force it truncates the file and tightens
// its mode. The directory must exist.
func WriteFile(path string, data []byte, force bool) error {
	flags := os.O_WRONLY | os.O_CREATE | os.O_EXCL
	if force {
		flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	}
	f, err := os.OpenFile(path, flags, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("%w: %s (use --force to replace it)", ErrExists, path)
	}
	if err != nil {
		return fmt.Errorf("export: %w", err)
	}
	if force {
		if err := f.Chmod(0o600); err != nil && !errors.Is(err, errors.ErrUnsupported) {
			_ = f.Close()
			return fmt.Errorf("export: %w", err)
		}
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return fmt.Errorf("export: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("export: %w", err)
	}
	return nil
}

// DefaultName is the file the TUI's :export writes: <slug>-<shortid>.<ext>
// with anything but letters, digits, dot, dash and underscore in the slug
// turned into a dash.
func DefaultName(s protocol.SessionSummary, f Format) string {
	slug := strings.Trim(strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			return r
		}
		return '-'
	}, s.Slug), "-.")
	if slug == "" {
		slug = "session"
	}
	return slug + "-" + s.ShortID + "." + f.Ext()
}
