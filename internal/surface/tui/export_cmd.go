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

// runExport is `:export [sel] [fmt]`: write the session (the selected one
// without sel) as a json (default), yaml, md or txt document to
// ~/.local/share/rex/exports/<slug>-<shortid>.<ext>, mode 0600, replacing an
// earlier export of the same session, and show the path. With one argument
// that names a format, it is the format; otherwise it is the selector.
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
	path, err := writeExport(sum, format)
	if err != nil {
		slog.Debug("tui: export failed", "session", id, "err", err)
		m.Err = "export: " + err.Error()
		return m, nil
	}
	m.Notice = displayName(sum) + ": exported to " + path + " (may hold secrets; keep it private)"
	return m, nil
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
// folder, returning the file's path.
func writeExport(sum protocol.SessionSummary, format export.Format) (string, error) {
	sessions, err := paths.SessionsDir()
	if err != nil {
		return "", err
	}
	dir, err := paths.ExportsDir()
	if err != nil {
		return "", err
	}
	doc, err := export.Build(export.Options{
		Session:        sum,
		TranscriptPath: filepath.Join(sessions, sum.ID, "transcript.log"),
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
	path := filepath.Join(dir, export.DefaultName(sum, format))
	if err := export.WriteFile(path, body, true); err != nil {
		return "", err
	}
	return path, nil
}
