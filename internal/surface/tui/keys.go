package tui

import (
	"log/slog"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// Help group titles shared by the tables and the help layout.
const (
	groupNavigation = "Navigation"
	groupActions    = "Actions"
	groupMouse      = "Mouse"
	groupFilter     = "Filter & search"
	groupModes      = "Modes"
	groupAttached   = "Attached session"
	groupSessionCmd = "Session commands"
	groupAppCmd     = "App commands"
)

// ctxMask says where a binding applies.
type ctxMask uint8

const (
	ctxBoard ctxMask = 1 << iota
	ctxAttach
	ctxMouse
)

// binding describes one key (or gesture) for the help overlay and the help
// line. It is descriptive only: key handling stays in update.go and
// chords.go, and TestBindingsAreHandled checks that every Press key is
// handled there.
type binding struct {
	Keys  string   // label shown in help, e.g. "j k"
	Desc  string   // help description
	Group string   // help group title
	Ctx   ctxMask  // where it applies
	Press []string // board keys that trigger it; a chord lists its first key then its second
	Line  string   // short label on the board help line ("" = not shown)
	Flag  string   // Features switch that gates the key ("" = always on)
}

// bindings is the key and gesture table, in help order.
var bindings = []binding{
	{Keys: "j k", Desc: "move row selection", Group: groupNavigation, Ctx: ctxBoard, Press: []string{"j", "k"}},
	{Keys: "g G", Desc: "top / bottom", Group: groupNavigation, Ctx: ctxBoard, Press: []string{"g", "G"}},
	{Keys: "tab", Desc: "next Ready session", Group: groupNavigation, Ctx: ctxBoard, Press: []string{"tab"}, Line: "next"},
	{Keys: sectionKeysLabel, Desc: "jump to Ready / Waiting / Working / Done (4 also folds a long Done column)", Group: groupNavigation, Ctx: ctxBoard, Press: []string{"1", "2", "3", "4"}},
	{Keys: "v", Desc: "simple / detailed rows (id, model, tokens, AI summary)", Group: groupNavigation, Ctx: ctxBoard, Press: []string{"v"}},

	{Keys: "t", Desc: "cycle tool filter", Group: groupFilter, Ctx: ctxBoard, Press: []string{"t"}, Flag: "board_filter"},
	{Keys: "/", Desc: "search the board (every word must match)", Group: groupFilter, Ctx: ctxBoard, Press: []string{"/"}, Flag: "search"},
	{Keys: "esc", Desc: "clear the search", Group: groupFilter, Ctx: ctxBoard, Press: []string{"esc"}, Flag: "search"},
	{Keys: "zh", Desc: "hide / show completed sections", Group: groupFilter, Ctx: ctxBoard, Press: []string{"z", "h"}, Flag: "board_filter"},
	{Keys: "z1-9", Desc: "hide / show section N", Group: groupFilter, Ctx: ctxBoard, Press: []string{"z", "1"}, Flag: "board_filter"},
	{Keys: "z0", Desc: "show every section", Group: groupFilter, Ctx: ctxBoard, Press: []string{"z", "0"}, Flag: "board_filter"},

	{Keys: "enter", Desc: "attach to selected session", Group: groupActions, Ctx: ctxBoard, Press: []string{"enter"}, Line: "open"},
	{Keys: "n", Desc: "new-agent wizard", Group: groupActions, Ctx: ctxBoard, Press: []string{"n"}},
	{Keys: "r", Desc: "rename selected (empty = slug)", Group: groupActions, Ctx: ctxBoard, Press: []string{"r"}},
	{Keys: "cc", Desc: "mark done / undo", Group: groupActions, Ctx: ctxBoard, Press: []string{"c", "c"}, Line: "done"},
	{Keys: "uc", Desc: "undo done (unmark, or resume an exited session)", Group: groupActions, Ctx: ctxBoard, Press: []string{"u", "c"}, Flag: "undo_complete"},
	{Keys: "C", Desc: "complete now (kill agent, mark done)", Group: groupActions, Ctx: ctxBoard, Press: []string{"C"}},
	{Keys: "dd", Desc: "delete selected", Group: groupActions, Ctx: ctxBoard, Press: []string{"d", "d"}},
	{Keys: "i", Desc: "focus λ prompt (new-session text)", Group: groupActions, Ctx: ctxBoard, Press: []string{"i"}, Line: "new"},
	{Keys: "S", Desc: "settings", Group: groupActions, Ctx: ctxBoard, Press: []string{"S"}},
	{Keys: "q", Desc: "quit (confirm)", Group: groupActions, Ctx: ctxBoard},

	{Keys: "click", Desc: "select row / filter chip", Group: groupMouse, Ctx: ctxMouse},
	{Keys: "double-click", Desc: "attach to row", Group: groupMouse, Ctx: ctxMouse},
	{Keys: "wheel", Desc: "move row selection", Group: groupMouse, Ctx: ctxMouse},
	{Keys: "hold ⌥ / ⇧", Desc: "select text natively", Group: groupMouse, Ctx: ctxMouse},

	{Keys: ":", Desc: "command mode", Group: groupModes, Ctx: ctxBoard, Press: []string{":"}},
	{Keys: "?", Desc: "this help", Group: groupModes, Ctx: ctxBoard, Press: []string{"?"}, Line: "more"},

	{Keys: "qq / ctrl+]", Desc: "detach", Group: groupAttached, Ctx: ctxAttach},
	{Keys: "::", Desc: "command bar (q detach, done, kill, name <t>, banner)", Group: groupAttached, Ctx: ctxAttach},
	{Keys: ":::", Desc: "type a literal \"::\" into the agent", Group: groupAttached, Ctx: ctxAttach},
	{Keys: "ctrl+pgup", Desc: "scroll back; k/j pgup/pgdn g/G, esc live", Group: groupAttached, Ctx: ctxAttach},
}

// helpLineKeys is the board help line, in order; each is the Keys of a
// binding that has a Line label.
var helpLineKeys = []string{"enter", "i", "cc", "tab", "?"}

// helpLineKeysMobile is the help line in the mobile layout.
var helpLineKeysMobile = []string{"enter", "i", "?"}

// bindingsIn returns the bindings of one help group as help rows, in table
// order.
func bindingsIn(group string) helpGroup {
	g := helpGroup{Title: group}
	for _, b := range bindings {
		if b.Group == group {
			g.Rows = append(g.Rows, helpRow{Key: b.Keys, Desc: b.Desc})
		}
	}
	return g
}

// cmdCall is one parsed `:` command line.
type cmdCall struct {
	Verb string   // the name typed, e.g. "fails"
	Args []string // whitespace-split arguments
	Line string   // the trimmed line, for commands that keep inner spacing
}

// command is one `:` command. Names are every verb that dispatches to it.
type command struct {
	Names []string
	Args  string // usage after the name in help, e.g. "<sel>"
	Label string // help key override; default ":" + Names joined by " :" + Args
	Desc  string
	Group string
	Flag  string // Features switch that gates the command ("" = always on)
	Run   func(m Model, c cmdCall) (Model, tea.Cmd)
}

// helpKey is the command's key column in the help overlay.
func (c command) helpKey() string {
	if c.Label != "" {
		return c.Label
	}
	k := ":" + strings.Join(c.Names, " :")
	if c.Args != "" {
		k += " " + c.Args
	}
	return k
}

// commands is the `:` command table, in help order within each group.
var commands = []command{
	{Names: []string{"new"}, Desc: "new-agent wizard", Group: groupSessionCmd, Run: runNew},
	{Names: []string{"name"}, Args: "[text]", Desc: "rename selected (no text: open editor)", Group: groupSessionCmd, Run: runName},
	{Names: []string{"rename"}, Args: "<s> <slug>", Desc: "change slug", Group: groupSessionCmd, Run: runRename},
	{Names: []string{"done"}, Args: "[sel]", Desc: "mark done", Group: groupSessionCmd, Run: runDone},
	{Names: []string{"undone"}, Args: "[sel]", Desc: "undo done", Group: groupSessionCmd, Run: runUndone},
	{Names: []string{"uc", "resume"}, Args: "[sel]", Desc: "undo done: unmark a running session, or resume an exited one", Group: groupSessionCmd, Flag: "undo_complete", Run: runUC},
	{Names: []string{"rid"}, Args: "[sel]", Desc: "show the full resume id", Group: groupSessionCmd, Flag: "resume", Run: runRID},
	{Names: []string{"complete"}, Args: "[sel]", Desc: "kill agent + mark done now", Group: groupSessionCmd, Run: runComplete},
	{Names: []string{"rm"}, Args: "<sel>", Desc: "delete", Group: groupSessionCmd, Run: runRm},
	{Names: []string{"prune"}, Desc: "clear Done (keeps sessions still working)", Group: groupSessionCmd, Run: runPrune},
	{Names: []string{"export"}, Args: "[sel] [fmt]", Desc: "write session to exports/", Group: groupSessionCmd, Flag: "session_export", Run: runExport},
	{Names: []string{"filter"}, Args: "[what]", Desc: "tool, or hide|show|only <section…>; no argument clears every filter", Group: groupSessionCmd, Flag: "board_filter", Run: runFilter},
	{Names: []string{"search"}, Args: "[text]", Desc: "search the board (every word must match); no text clears", Group: groupSessionCmd, Flag: "search", Run: runSearch},

	{Names: []string{"q", "quit"}, Desc: "quit (confirm)", Group: groupAppCmd, Run: runQuit},
	{Names: []string{"q!"}, Desc: "force quit", Group: groupAppCmd, Run: runForceQuit},
	{Names: []string{"bg", "detach"}, Desc: "detach, save place", Group: groupAppCmd, Run: runDetach},
	{Names: []string{"reload"}, Desc: "reload tools.yaml", Group: groupAppCmd, Run: runReload},
	{Names: []string{"fail", "fails", "failed"}, Label: ":fail", Desc: "failed sessions + daemon log (:fails :failed)", Group: groupAppCmd, Run: runFail},
	{Names: []string{"stats"}, Desc: "session statistics overlay", Group: groupAppCmd, Run: runStats},
	{Names: []string{"usage"}, Desc: "usage per agent and model, quota, resets", Group: groupAppCmd, Flag: "usage", Run: runUsage},
	{Names: []string{"overview"}, Desc: "what is ready for you, what to review, what finished", Group: groupAppCmd, Run: runOverview},
	{Names: []string{"border"}, Args: "[color]", Desc: "window border for this run: on, off, next, random, a color name or #rrggbb", Group: groupAppCmd, Run: runBorder},
	{Names: []string{"help"}, Desc: "this overlay", Group: groupAppCmd, Run: runHelp},
	{Names: []string{"settings"}, Desc: "open settings", Group: groupAppCmd, Run: runSettings},
}

// lookupCommand finds the command a verb dispatches to.
func lookupCommand(verb string) (command, bool) {
	for _, c := range commands {
		for _, n := range c.Names {
			if n == verb {
				return c, true
			}
		}
	}
	return command{}, false
}

// commandsIn returns the commands of one help group as help rows.
func commandsIn(group string) helpGroup {
	g := helpGroup{Title: group}
	for _, c := range commands {
		if c.Group == group {
			g.Rows = append(g.Rows, helpRow{Key: c.helpKey(), Desc: c.Desc})
		}
	}
	return g
}

func runQuit(m Model, _ cmdCall) (Model, tea.Cmd) {
	m.Focus = FocusConfirmQuit
	return m, nil
}

func runForceQuit(m Model, _ cmdCall) (Model, tea.Cmd) {
	m.Quitting = true
	return m, tea.Quit
}

func runDetach(m Model, _ cmdCall) (Model, tea.Cmd) {
	_ = SaveTUIState(m)
	m.Quitting = true
	return m, tea.Quit
}

func runHelp(m Model, _ cmdCall) (Model, tea.Cmd) { return openHelp(m), nil }

func runSettings(m Model, _ cmdCall) (Model, tea.Cmd) { return openSettings(m) }

func runReload(m Model, _ cmdCall) (Model, tea.Cmd) { return m, reloadDaemonCmd(m) }

func runFilter(m Model, c cmdCall) (Model, tea.Cmd) { return filterCommand(m, c.Args), nil }

func runPrune(m Model, _ cmdCall) (Model, tea.Cmd) { return openPrune(m) }

func runRm(m Model, c cmdCall) (Model, tea.Cmd) {
	if len(c.Args) == 1 {
		id, msg := resolveLocal(m, c.Args[0])
		if id == "" {
			m.Err = msg
			return m, nil
		}
		return m, deleteSessionCmd(m.Client, id)
	}
	m.Err = "rm: selector required"
	return m, nil
}

func runRename(m Model, c cmdCall) (Model, tea.Cmd) {
	if len(c.Args) == 2 {
		id, msg := resolveLocal(m, c.Args[0])
		if id == "" {
			m.Err = msg
			return m, nil
		}
		if err := protocol.ValidateSlug(c.Args[1]); err != nil {
			m.Err = "rename: " + err.Error()
			return m, nil
		}
		return m, renameCmd(m.Client, id, c.Args[1])
	}
	m.Err = "rename: <selector> <new-slug>"
	return m, nil
}

// runName: `:name` opens the rename editor for the selection; `:name <text…>`
// sets it directly.
func runName(m Model, c cmdCall) (Model, tea.Cmd) {
	if len(c.Args) == 0 {
		return openRename(m)
	}
	if m.SelectedID == "" || indexOfSelected(m) < 0 {
		m.Err = "name: no session selected"
		return m, nil
	}
	name := strings.TrimSpace(strings.TrimPrefix(c.Line, c.Verb))
	return m, setNameCmd(namerOf(m), m.SelectedID, name)
}

func runDone(m Model, c cmdCall) (Model, tea.Cmd) {
	id, ok := commandTarget(&m, c.Verb, c.Args)
	if !ok {
		return m, nil
	}
	return toggleRetire(m, id)
}

func runUndone(m Model, c cmdCall) (Model, tea.Cmd) {
	id, ok := commandTarget(&m, c.Verb, c.Args)
	if !ok {
		return m, nil
	}
	return unmarkDone(m, id)
}

// unmarkDone only unmarks: toggling here would mark an unmarked session.
// Shared by the board's :undone and the attach popup's ::undone.
func unmarkDone(m Model, id string) (Model, tea.Cmd) {
	if sum, found := findSession(m.Sessions, id); found && sum.RetireAt.IsZero() {
		m.Notice = displayName(sum) + " is not marked done"
		return m, nil
	}
	return toggleRetire(m, id)
}

func runComplete(m Model, c cmdCall) (Model, tea.Cmd) {
	id, ok := commandTarget(&m, c.Verb, c.Args)
	if !ok {
		return m, nil
	}
	return m, completeSessionCmd(m.Client, id)
}

// runNew takes the same path as the `n` key: setting FocusWizard alone left
// Wizard nil and soft-locked the TUI (P0-07).
func runNew(m Model, _ cmdCall) (Model, tea.Cmd) { return openWizard(m) }

func runFail(m Model, _ cmdCall) (Model, tea.Cmd) { return openFail(m) }

func runStats(m Model, _ cmdCall) (Model, tea.Cmd) {
	if m.Focus == FocusStats {
		// Toggle off.
		m.Focus = FocusBoard
		slog.Debug("tui: stats overlay closed")
	} else {
		m.Focus = FocusStats
		slog.Debug("tui: stats overlay opened")
	}
	return m, nil
}
