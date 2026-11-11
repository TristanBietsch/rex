package export

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
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

// afterStat is a test hook run between sizing the transcript and reading it.
var afterStat = func() {}

// Build reads the session's transcript and returns the document. The file
// is opened read-only and read up to the size it has at that moment, so a
// running session keeps writing undisturbed and bytes appended meanwhile are
// left for the next export. Full-screen tools are replayed (Frames), line
// tools stripped (Strip).
func Build(o Options) (Document, error) {
	if o.MaxFrames < 0 {
		return Document{}, fmt.Errorf("export: max frames %d: must not be negative", o.MaxFrames)
	}
	d := Document{Version: Version, Session: cleanSession(o.Session), Source: SourceFrames}
	o.Session = d.Session
	if o.FromAgentLog {
		turns, notice, err := agentTurns(o)
		if err != nil {
			return d, err
		}
		if notice == "" {
			d.Source = SourceAgentLog
			if o.MaxFrames > 0 && len(turns) > o.MaxFrames {
				d.TurnsDropped = len(turns) - o.MaxFrames
				turns = turns[d.TurnsDropped:]
			}
			d.Turns = turns
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
	afterStat()
	r := io.LimitReader(f, st.Size())

	if LineTool(o.Session.ToolID) {
		text, err := Strip(r)
		if err != nil {
			return d, err
		}
		d.Frames, d.FramesDropped = lineFrames(text, st.Size(), o.MaxFrames)
		return d, nil
	}
	cols, rows := o.Cols, o.Rows
	if cols == 0 {
		cols = DefaultCols
	}
	if rows == 0 {
		rows = DefaultRows
	}
	d.Frames, d.FramesDropped, err = FramesLimited(r, cols, rows, o.Cuts, o.MaxFrames)
	return d, err
}

// lineFrames splits a line tool's text into frames of scrollFrameLines lines
// (At is the transcript size for all of them: the stripped text has no
// offsets), keeping the last limit when limit > 0.
func lineFrames(text string, size int64, limit int) (frames []Frame, dropped int) {
	if text == "" {
		return nil, 0
	}
	lines := strings.Split(text, "\n")
	for len(lines) > 0 {
		n := min(scrollFrameLines, len(lines))
		frames = append(frames, Frame{At: size, Text: strings.Join(lines[:n], "\n")})
		lines = lines[n:]
	}
	if limit > 0 && len(frames) > limit {
		dropped = len(frames) - limit
		frames = frames[dropped:]
	}
	return frames, dropped
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

// ErrSymlink reports an output path that is a symbolic link.
var ErrSymlink = errors.New("refusing to write through a symbolic link")

// WriteFile writes data to path with mode 0600. Without force it refuses to
// replace a file (ErrExists). With force it replaces the file atomically (a
// temporary file in the same folder, renamed over path) and refuses a path
// that is a symbolic link (ErrSymlink). The folder must exist.
func WriteFile(path string, data []byte, force bool) error {
	if !force {
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("%w: %s (use --force to replace it)", ErrExists, path)
		}
		if err != nil {
			return fmt.Errorf("export: %w", err)
		}
		return writeAndClose(f, data)
	}
	if st, err := os.Lstat(path); err == nil && st.Mode()&fs.ModeSymlink != 0 {
		return fmt.Errorf("export: %w: %s", ErrSymlink, path)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".rex-export-*") // O_EXCL, mode 0600
	if err != nil {
		return fmt.Errorf("export: %w", err)
	}
	if err := writeAndClose(tmp, data); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("export: %w", err)
	}
	return nil
}

func writeAndClose(f *os.File, data []byte) error {
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
