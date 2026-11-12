package core

// Class is how a command behaves, which decides the output formats it
// supports (see docs/cli.md).
type Class uint8

// Command classes.
const (
	// ClassQuery commands read state: all five formats.
	ClassQuery Class = iota
	// ClassAction commands change state and report a result record: all five.
	ClassAction
	// ClassContent commands print raw content: text and txt (agent behaves
	// like txt).
	ClassContent
	// ClassInteractive commands need a terminal: text only.
	ClassInteractive
)

var classNames = [...]string{"query", "action", "content", "interactive"}

func (c Class) String() string { return classNames[c] }

// Command describes one rex command for help, the agent card and shell
// completion. The table below is the single list: `rex --help`, `rex <cmd>
// -h` and the completion scripts are built from it.
type Command struct {
	Name    string
	Aliases []string
	// Section groups the command in `rex --help`.
	Section string
	// Args is the synopsis after the command name ("<sel> [flags]").
	Args string
	// Short is the one-line description of the command list.
	Short string
	// Long is the sentence or two shown by `rex <cmd> -h` (Short when empty).
	Long string
	// Example is one runnable line shown by `rex <cmd> -h`.
	Example string
	Class   Class
}

// Help section names, in display order.
const (
	SectionSessions  = "Sessions"
	SectionDaemon    = "Daemon"
	SectionInsights  = "Insights"
	SectionLifecycle = "Lifecycle"
	SectionOther     = "Other"
)

// HelpSections is the order of sections in `rex --help`.
var HelpSections = []string{SectionSessions, SectionDaemon, SectionInsights, SectionLifecycle, SectionOther}

// Commands lists every command in help order. TestRun_EveryCommandIsKnown
// keeps it in step with the dispatcher in cmd/rex.
var Commands = []Command{
	{Name: "ls", Section: SectionSessions, Args: "[flags]", Short: "list sessions",
		Long:    "List sessions, newest state first. -wide adds start and end times and the resume id.",
		Example: "rex ls -state needs_input", Class: ClassQuery},
	{Name: "new", Section: SectionSessions, Args: "[flags] -- <prompt>", Short: "start a session (-tool -model -cwd)",
		Long:    "Start a session in the daemon and print its short id and slug. Put -- before a prompt that starts with -.",
		Example: "rex new -tool claude -model opus -- fix the failing test", Class: ClassAction},
	{Name: "attach", Section: SectionSessions, Args: "<sel> [flags]", Short: "open a session (ctrl+] detaches)",
		Long:    "Open a session's terminal. Needs a terminal; ctrl+] detaches.",
		Example: "rex attach fix-auth", Class: ClassInteractive},
	{Name: "reply", Section: SectionSessions, Args: "<sel> [flags] -- <text>", Short: "answer a session that needs input",
		Long:    "Type text and Enter into a session that needs input.",
		Example: "rex reply fix-auth -- yes, go ahead", Class: ClassAction},
	{Name: "send", Section: SectionSessions, Args: "<sel> [flags] -- <text>", Short: "send raw input (stdin when no text)",
		Long:    "Send text to a session exactly as given, with no Enter added; reads stdin when there is no text.",
		Example: "rex send fix-auth -- hello", Class: ClassAction},
	{Name: "wait", Section: SectionSessions, Args: "<sel> [flags]", Short: "block until a session reaches a state",
		Long:    "Block until the session reaches -until (default done). Exits 7 on -timeout, 2 if the session is removed.",
		Example: "rex wait fix-auth -until needs_input -timeout 10m", Class: ClassAction},
	{Name: "rename", Section: SectionSessions, Args: "<sel> <slug>", Short: "change a session's slug",
		Example: "rex rename 7d4f fix-auth", Class: ClassAction},
	{Name: "name", Section: SectionSessions, Args: "<sel> [-clear] [-- <text>]", Short: "set, show or clear the display name",
		Long:    "Set the name the board shows, print it when there is no text, or clear it (the board then shows the slug).",
		Example: "rex name fix-auth -- Auth fix", Class: ClassAction},
	{Name: "done", Aliases: []string{"retire"}, Section: SectionSessions, Args: "<sel> [-after 72h]", Short: "mark done; cleared after retire_after_hours",
		Long:    "Mark a session done without stopping it. After -after (default retire_after_hours) the daemon removes it.",
		Example: "rex done fix-auth -after 24h", Class: ClassAction},
	{Name: "undone", Aliases: []string{"unretire"}, Section: SectionSessions, Args: "<sel>", Short: "cancel a pending done",
		Example: "rex undone fix-auth", Class: ClassAction},
	{Name: "archive", Section: SectionSessions, Args: "<sel>", Short: "archive a session",
		Example: "rex archive fix-auth", Class: ClassAction},
	{Name: "resume", Section: SectionSessions, Args: "<sel> [-print]", Short: "respawn an exited session (-print: only the resume id)",
		Long:    "Respawn an exited session in place, or unmark a running one marked done. -print prints the full resume id and sends nothing.",
		Example: "rex resume fix-auth", Class: ClassAction},
	{Name: "undo", Section: SectionSessions, Args: "<sel> [-turns N] [-yes]", Short: "restore the folder to before the last turn",
		Long:    "Restore the session's folder to its state before the last agent turn (needs turn_snapshots). Asks first; without a terminal it needs -yes.",
		Example: "rex undo fix-auth -turns 1", Class: ClassAction},
	{Name: "redo", Section: SectionSessions, Args: "<sel> [-yes]", Short: "bring back what undo replaced",
		Example: "rex redo fix-auth", Class: ClassAction},
	{Name: "complete", Section: SectionSessions, Args: "<sel>", Short: "stop a session now and mark it done",
		Example: "rex complete fix-auth", Class: ClassAction},
	{Name: "rm", Section: SectionSessions, Args: "<sel> [-force]", Short: "delete a session (-force without a terminal)",
		Long:    "Delete a session and its files. Asks first; without a terminal it needs -force.",
		Example: "rex rm fix-auth -force", Class: ClassAction},
	{Name: "prune", Section: SectionSessions, Args: "[-dry-run] [-yes]", Short: "delete Completed sessions",
		Long:    "Delete every session in the Completed column, keeping any still working. Asks first; without a terminal it needs -yes.",
		Example: "rex prune -dry-run", Class: ClassAction},
	{Name: "log", Section: SectionSessions, Args: "<sel> [flags]", Short: "print a session's transcript (-f follows)",
		Long:    "Print the session transcript with ANSI stripped; -bytes keeps the raw bytes.",
		Example: "rex log fix-auth -n 50", Class: ClassContent},

	{Name: "status", Section: SectionDaemon, Args: "[flags]", Short: "session counts; exit 1 if one needs input",
		Long:    "Print the number of sessions per state. Exits 1 when a session needs input.",
		Example: "rex status", Class: ClassQuery},
	{Name: "daemon", Section: SectionDaemon, Args: "start|stop|restart|status|logs", Short: "manage the rex-daemon",
		Long:    "Start, stop, restart or check the background daemon, or show its log. status exits 4 when it is not running.",
		Example: "rex daemon status", Class: ClassAction},
	{Name: "reload", Section: SectionDaemon, Args: "[flags]", Short: "reload tools.yaml in the daemon",
		Example: "rex reload", Class: ClassAction},

	{Name: "errors", Section: SectionInsights, Args: "[-n N] [-since 24h] [-clear]", Short: "failed rex commands from the error log",
		Long:    "Show the private log of failed rex commands, newest first. -clear deletes it. Turn the log off with cli_error_log or REX_ERROR_LOG=0.",
		Example: "rex errors -n 5", Class: ClassQuery},
	{Name: "overview", Section: SectionInsights, Args: "[flags]", Short: "one-screen summary of every session by what needs you",
		Example: "rex overview", Class: ClassQuery},
	{Name: "export", Section: SectionInsights, Args: "<sel> [flags]", Short: "write a session as a readable document",
		Long:    "Write one session's metadata and cleaned transcript as json, yaml, md or txt to stdout or --out. Nothing is uploaded.",
		Example: "rex export fix-auth -o md --out fix-auth.md", Class: ClassContent},
	{Name: "usage", Section: SectionInsights, Args: "[flags]", Short: "tokens, cost and plan quota per agent and model",
		Long:    "Show token use, cost and plan quota per agent and model. -range limits the window, -tool filters, -by groups.",
		Example: "rex usage -range 7d -by model", Class: ClassQuery},
	{Name: "digest", Section: SectionInsights, Args: "[flags]", Short: "today's sessions, time and totals",
		Example: "rex digest -since 48h", Class: ClassQuery},
	{Name: "stats", Section: SectionInsights, Args: "[flags]", Short: "lifetime usage by model and tool",
		Example: "rex stats -all", Class: ClassQuery},
	{Name: "fleet", Section: SectionInsights, Args: "ls|show|set|unset", Short: "list or edit session fleets",
		Example: "rex fleet ls", Class: ClassQuery},

	{Name: "setup", Section: SectionLifecycle, Args: "[flags]", Short: "guided first-run wizard (-yes for defaults)",
		Long:    "Pick a default tool, model and effort and install the shell profile block. -yes accepts the defaults; -preview shows what it would do.",
		Example: "rex setup -yes", Class: ClassInteractive},
	{Name: "doctor", Section: SectionLifecycle, Args: "[flags]", Short: "check the install; exit 1 if a check fails",
		Example: "rex doctor -verbose", Class: ClassQuery},
	{Name: "update", Section: SectionLifecycle, Args: "[flags]", Short: "upgrade rex in place (-check only looks)",
		Example: "rex update -check", Class: ClassAction},
	{Name: "uninstall", Section: SectionLifecycle, Args: "[flags]", Short: "remove rex and optional state",
		Long:    "Remove the binaries and the profile block; -wipe-state and -purge-config also remove data. -dry-run shows the plan.",
		Example: "rex uninstall -dry-run", Class: ClassAction},

	{Name: "skills", Section: SectionOther, Args: "[flags]", Short: "list the skills library (~/.config/rex/skills)",
		Example: "rex skills -o json", Class: ClassQuery},
	{Name: "config", Section: SectionOther, Args: "list|get|set|reset|edit", Short: "view and change settings",
		Example: "rex config set skip_permissions false", Class: ClassQuery},
	{Name: "render", Section: SectionOther, Args: "[flags]", Short: "render an event stream from stdin",
		Example: "rex render -w 80 -h 24", Class: ClassContent},
	{Name: "completion", Section: SectionOther, Args: "bash|zsh|fish", Short: "print a shell completion script",
		Example: "source <(rex completion zsh)", Class: ClassContent},
	{Name: "version", Section: SectionOther, Args: "", Short: "print the version",
		Example: "rex version", Class: ClassQuery},
	{Name: "agent-help", Section: SectionOther, Args: "", Short: "the usage card for LLM agents",
		Example: "rex agent-help", Class: ClassQuery},
}

// FindCommand returns the command called name (or one of its aliases).
func FindCommand(name string) (Command, bool) {
	for _, c := range Commands {
		if c.Name == name {
			return c, true
		}
		for _, a := range c.Aliases {
			if a == name {
				return c, true
			}
		}
	}
	return Command{}, false
}

// CommandNames lists every command name and alias, in help order, for
// "did you mean" and shell completion. "help" is included.
func CommandNames() []string {
	names := []string{"help"}
	for _, c := range Commands {
		names = append(names, c.Name)
		names = append(names, c.Aliases...)
	}
	return names
}
