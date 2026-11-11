package tui

import (
	"log/slog"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tristanbietsch/rex/internal/features/export"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// exportDoneMsg reports a finished background export.
type exportDoneMsg struct {
	ID   string
	Name string
	Path string
	Err  error
}

// runExport is `:export [sel] [fmt]`: write the session (the selected one
// without sel) as a json (default), yaml, md or txt document to
// ~/.local/share/rex/exports/<slug>-<shortid>.<ext>, mode 0600, replacing an
// earlier export of the same session. The work runs in the background (a
// large transcript takes a while) and exportDoneMsg shows the path. With one
// argument that names a format, it is the format; otherwise the selector.
func runExport(m Model, c cmdCall) (Model, tea.Cmd) {
	sel, format, ok := exportArgs(&m, c.Args)
	if !ok {
		return m, nil
	}
	id, ok := commandTarget(&m, c.Verb, sel)
	if !ok {
		return m, nil
	}
	sum, found := findSession(m.Sessions, id)
	if !found {
		m.Err = "export: session is gone"
		return m, nil
	}
	name := displayName(sum)
	if m.exporting[id] {
		m.Notice = "export of " + name + " is already running"
		return m, nil
	}
	sessions, err := paths.SessionsDir()
	if err != nil {
		m.Err = "export: " + err.Error()
		return m, nil
	}
	exports, err := paths.ExportsDir()
	if err != nil {
		m.Err = "export: " + err.Error()
		return m, nil
	}
	// A fresh map: Model is copied by value and the old map may be shared.
	busy := map[string]bool{id: true}
	for k := range m.exporting {
		busy[k] = true
	}
	m.exporting = busy
	m.Err = ""
	m.Notice = "exporting " + name + "…"
	return m, func() tea.Msg {
		path, err := writeExport(sum, format, sessions, exports)
		return exportDoneMsg{ID: id, Name: name, Path: path, Err: err}
	}
}

// applyExportDone ends a background export: the path as a notice, or the
// error on the status line.
func applyExportDone(m Model, msg exportDoneMsg) Model {
	busy := map[string]bool{}
	for k := range m.exporting {
		if k != msg.ID {
			busy[k] = true
		}
	}
	m.exporting = busy
	if msg.Err != nil {
		slog.Debug("tui: export failed", "session", msg.ID, "err", msg.Err)
		m.Err = "export: " + msg.Err.Error()
		return m
	}
	m.Notice = "exported " + msg.Path + " (private, may hold secrets)"
	return m
}

// exportArgs splits `:export` arguments into the selector (nil when absent)
// and the format.
func exportArgs(m *Model, args []string) (sel []string, format export.Format, ok bool) {
	format = export.FormatJSON
	switch len(args) {
	case 0:
	case 1:
		if f, err := export.ParseFormat(args[0]); err == nil {
			format = f
		} else {
			sel = args
		}
	case 2:
		f, err := export.ParseFormat(args[1])
		if err != nil {
			m.Err = "export: " + err.Error()
			return nil, "", false
		}
		sel, format = args[:1], f
	default:
		m.Err = "usage: :export [sel] [json|yaml|md|txt]"
		return nil, "", false
	}
	return sel, format, true
}

// writeExport builds the export of sum and writes it into the exports
// folder, returning the file's path. The folders are resolved by the caller
// so this can run off the UI goroutine.
func writeExport(sum protocol.SessionSummary, format export.Format, sessionsDir, dir string) (string, error) {
	doc, err := export.Build(export.Options{
		Session:        sum,
		TranscriptPath: filepath.Join(sessionsDir, sum.ID, "transcript.log"),
	})
	if err != nil {
		return "", err
	}
	body, err := export.Render(doc, format)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, export.DefaultName(sum, format))
	if err := export.WriteFile(path, body, true); err != nil {
		return "", err
	}
	return path, nil
}
