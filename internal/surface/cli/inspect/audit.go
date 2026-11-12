package inspect

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/features/audit"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/workspace"
)

// auditScope is where `rex audit` reads, and why.
type auditScope struct {
	files []string
	// label names the scope in the header ("global", "workspace 7f3b89a2 api", "all").
	label string
	// notes are warnings found while resolving it (an unsafe workspace).
	notes []string
}

// RunAudit lists finished sessions from the audit trail, newest first.
//
//	rex audit [--ws <id|path> | --all] [--since 7d] [--tool T] [--state S] [-o FORMAT]
//
// With no scope flag it reads the workspace around the current folder when
// the workspaces setting is on, else the global trail. It reads the files
// itself (the daemon only writes them). Exit 5 while audit_trail is off.
func RunAudit(args []string) error {
	fs := flag.NewFlagSet("audit", flag.ContinueOnError)
	out := core.AddOutputFlags(fs, core.AllFormats)
	wsRef := fs.String("ws", "", "read this workspace's trail (an id or a folder)")
	all := fs.Bool("all", false, "read the global trail and every known workspace's")
	since := fs.String("since", "", "only sessions that ended after this: 7d, 12h, 90m or a date")
	tool := fs.String("tool", "", "only this tool id")
	stateF := fs.String("state", "", "only this end state: done, failed or crashed")
	rest, err := out.Parse(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 0 {
		return core.Errorf(core.ExitInvalidArgs, "rex audit -h", "audit: unexpected argument %q", rest[0])
	}
	if *all && *wsRef != "" {
		return core.Errorf(core.ExitInvalidArgs, "rex audit -h", "audit: --ws and --all cannot be combined")
	}
	if *stateF != "" && !validAuditState(*stateF) {
		return core.Errorf(core.ExitInvalidArgs, "rex audit --state failed", "audit: --state must be done, failed or crashed, not %q", *stateF)
	}
	p := core.Out()
	now := p.Now()
	cutoff, err := audit.ParseSince(*since, now)
	if err != nil {
		return core.Errorf(core.ExitInvalidArgs, "rex audit --since 7d", "audit: %w", err)
	}
	if err := core.RequireFeature("audit_trail"); err != nil {
		return err
	}

	store, _, _ := settings.LoadDefault()
	wsOn := settings.FeatureOn(store, "workspaces")
	if *wsRef != "" && !wsOn {
		return core.OffError("workspaces")
	}
	scope, err := resolveAuditScope(*wsRef, *all, wsOn)
	if err != nil {
		return err
	}

	recs, errs := audit.Read(scope.files, audit.Filter{Since: cutoff, Tool: *tool, State: *stateF})
	for _, e := range errs {
		if errors.Is(e, audit.ErrTruncated) {
			scope.notes = append(scope.notes, "older records not shown: "+e.Error())
			continue
		}
		scope.notes = append(scope.notes, "skipped: "+e.Error())
	}
	reverse(recs) // newest first
	if recs == nil {
		recs = []audit.Record{}
	}
	return p.Emit(core.Result{Cmd: "audit", Data: recs, View: auditView(recs, scope, p.Now().Location())})
}

func validAuditState(s string) bool { return s == "done" || s == "failed" || s == "crashed" }

func reverse[T any](s []T) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}

// resolveAuditScope picks the files to read.
func resolveAuditScope(wsRef string, all, wsOn bool) (auditScope, error) {
	global, err := paths.AuditFile()
	if err != nil {
		return auditScope{}, core.Errorf(core.ExitGeneric, "", "audit: %w", err)
	}
	if !wsOn {
		return auditScope{files: []string{global}, label: "global"}, nil
	}
	home, err := paths.Home()
	if err != nil {
		return auditScope{}, core.Errorf(core.ExitGeneric, "", "audit: %w", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return auditScope{}, core.Errorf(core.ExitGeneric, "", "audit: %w", err)
	}
	regPath, err := paths.WorkspacesFile()
	if err != nil {
		return auditScope{}, core.Errorf(core.ExitGeneric, "", "audit: %w", err)
	}
	reg, err := workspace.OpenRegistry(regPath)
	if err != nil {
		return auditScope{}, core.Errorf(core.ExitGeneric, "", "audit: %w", err)
	}

	switch {
	case wsRef != "":
		w, err := workspace.Resolve(wsRef, cwd, home, reg)
		if err != nil {
			return auditScope{}, auditWorkspaceError(err)
		}
		return workspaceScope(w), nil
	case all:
		sc := auditScope{files: []string{global}, label: "all"}
		es, err := reg.List()
		if err != nil {
			sc.notes = append(sc.notes, "workspace registry unreadable: "+err.Error())
		}
		for _, e := range es {
			// Load applies the same safety rules as discovery: a .rex/ that is
			// a symlink or writable by others is not read.
			w, lerr := workspace.Load(e.Root)
			switch {
			case lerr != nil:
				sc.notes = append(sc.notes, fmt.Sprintf("skipped workspace %s: %v", e.ID, lerr))
			case w != nil && w.ID == e.ID:
				sc.files = append(sc.files, auditFileOf(w))
			}
		}
		return sc, nil
	}
	w, err := workspace.Find(cwd, home)
	switch {
	case err != nil:
		sc := auditScope{files: []string{global}, label: "global"}
		sc.notes = append(sc.notes, "this folder's .rex/ was not used: "+err.Error())
		return sc, nil //nolint:nilerr // an unusable .rex/ is a note, and the global trail still answers
	case w != nil:
		return workspaceScope(w), nil
	}
	return auditScope{files: []string{global}, label: "global"}, nil
}

func auditFileOf(w *workspace.Workspace) string {
	return filepath.Join(w.Root, workspace.DirName, workspace.AuditFileName)
}

func workspaceScope(w *workspace.Workspace) auditScope {
	return auditScope{files: []string{auditFileOf(w)}, label: "workspace " + w.ID + " " + w.Name}
}

func auditWorkspaceError(err error) error {
	switch {
	case errors.Is(err, workspace.ErrNotFound), errors.Is(err, workspace.ErrMoved):
		return core.Errorf(core.ExitSelectorNotFound, "rex ws ls", "audit: %w", err)
	case errors.Is(err, workspace.ErrUnsafe), errors.Is(err, workspace.ErrInvalid):
		return core.Errorf(core.ExitOperationRefused, "", "audit: %w", err)
	}
	return core.Errorf(core.ExitGeneric, "", "audit: %w", err)
}

// auditView lays the records out: one row each, newest first.
func auditView(recs []audit.Record, scope auditScope, loc *time.Location) core.View {
	agent := core.SetOf(core.FormatAgent)
	cols := []core.Col{
		{Key: "ended_at", Title: "Ended"},
		{Key: "state", Title: "State"},
		{Key: "tool", Title: "Tool"},
		{Key: "model", Title: "Model", DropBelow: 100},
		{Key: "name", Title: "Session", Flex: true, Min: 8, Max: 24},
		{Key: "duration", Title: "Took", Align: core.AlignRight},
		{Key: "workspace_id", Title: "Workspace", DropBelow: 90},
		{Key: "session_id", Title: "Session id", Formats: agent},
		{Key: "prompt", Title: "Prompt", Flex: true, Min: 10, Max: 60, Below: true},
	}
	rows := make([][]core.Cell, len(recs))
	for i, r := range recs {
		rows[i] = []core.Cell{
			{Text: r.EndedAt.In(loc).Format("2006-01-02 15:04"), Raw: r.EndedAt.UTC().Format(time.RFC3339)},
			{Text: r.State, Tone: auditTone(r.State)},
			{Text: r.Tool},
			{Text: r.Model},
			{Text: nonEmpty(r.Name, r.ShortID)},
			{Text: spanText(r.DurationS), Raw: r.DurationS},
			{Text: r.WorkspaceID},
			{Text: r.SessionID},
			{Text: r.PromptFirstLine},
		}
	}
	v := core.View{core.Header{Title: "Audit", Meta: fmt.Sprintf("%s · %d sessions", scope.label, len(recs))}}
	for _, n := range scope.notes {
		v = append(v, core.Note{Tone: core.ToneWarn, Text: n})
	}
	return append(v, core.Table{Kind: "audit", Cols: cols, Rows: rows, Empty: "no audit records in this scope"})
}

func auditTone(state string) core.Tone {
	switch state {
	case "done":
		return core.ToneDone
	case "failed":
		return core.ToneFailed
	case "crashed":
		return core.ToneCrashed
	}
	return core.ToneNone
}

func nonEmpty(s, fallback string) string {
	if s != "" {
		return s
	}
	return fallback
}

// spanText is a duration in seconds as 45s, 12m, 3h05m.
func spanText(sec int64) string {
	switch {
	case sec < 60:
		return strconv.FormatInt(sec, 10) + "s"
	case sec < 3600:
		return strconv.FormatInt(sec/60, 10) + "m"
	}
	return fmt.Sprintf("%dh%02dm", sec/3600, sec%3600/60)
}
