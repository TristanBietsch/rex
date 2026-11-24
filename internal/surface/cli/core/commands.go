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
	// Args is the synopsis after the command name ("<sel> [flags]"), as
	// `rex <cmd> -h` shows it.
	Args string
	// Brief is the short argument hint in the `rex --help` overview
	// ("<sel> [-f] [-n N]"), at most 22 columns; "" for none.
	Brief string
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
	SectionStart  = "Start & steer"
	SectionWatch  = "Watch"
	SectionManage = "Manage"
	SectionDaemon = "Daemon"
	SectionSetup  = "Setup"
)

// HelpSections is the order of sections in `rex --help`.
var HelpSections = []string{SectionStart, SectionWatch, SectionManage, SectionDaemon, SectionSetup}

// Commands lists every command in help order. TestRun_EveryCommandIsKnown
// keeps it in step with the dispatcher in cmd/rex.
var Commands = []Command{
	{Name: "new", Section: SectionStart, Args: "[flags] -- <prompt>", Brief: "[flags] -- <prompt>", Short: "start an agent session",
		Long:    "Start a session in the daemon and print its short id and slug. Put -- before a prompt that starts with -.",
		Example: "rex new -tool claude -model opus -- fix the failing test", Class: ClassAction},
	{Name: "attach", Section: SectionStart, Args: "<sel> [flags]", Brief: "<sel>", Short: "open a session (ctrl+] detaches)",
		Long:    "Open a session's terminal. Needs a terminal; ctrl+] detaches.",
		Example: "rex attach fix-auth", Class: ClassInteractive},
	{Name: "reply", Section: SectionStart, Args: "<sel> [flags] -- <text>", Brief: "<sel> -- <text>", Short: "answer a session that needs input",
		Long:    "Type text and Enter into a session that needs input.",
		Example: "rex reply fix-auth -- yes, go ahead", Class: ClassAction},
	{Name: "send", Section: SectionStart, Args: "<sel> [flags] -- <text>", Brief: "<sel> -- <text>", Short: "send raw input (stdin when no text)",
		Long:    "Send text to a session exactly as given, with no Enter added; reads stdin when there is no text.",
		Example: "rex send fix-auth -- hello", Class: ClassAction},
	{Name: "wait", Section: SectionStart, Args: "<sel> [flags]", Brief: "<sel> -until STATE", Short: "block until a session reaches a state",
		Long:    "Block until the session reaches -until (default done). Exits 7 on -timeout, 2 if the session is removed.",
		Example: "rex wait fix-auth -until needs_input -timeout 10m", Class: ClassAction},
	{Name: "loop", Section: SectionStart, Args: "<subcommand> [args]", Brief: "start|ls|stop|…", Short: "re-send a prompt until a check passes",
		Long:    "Run a session in a loop: rex re-sends your prompt each time the agent finishes a turn, until a check command exits 0 or a cap is reached. Needs the loops feature. start <name> [-cwd DIR] runs ~/.config/rex/loops/<name>.yaml; new <name> writes a starter file; log <loop-id> shows what happened.",
		Example: "rex loop start fix-until-green -cwd ~/code/app", Class: ClassAction},
	{Name: "here", Section: SectionStart, Args: "", Brief: "", Short: "open this workspace's board",
		Long:    "Open the board showing only the sessions of the workspace around the current folder, with its id and name in the header and its color on the window border. New sessions start at the workspace root. Exits 5 when there is no .rex/ above this folder. Needs workspaces.",
		Example: "rex here", Class: ClassInteractive},

	{Name: "ls", Section: SectionWatch, Args: "[flags]", Brief: "[-state S] [-tool T]", Short: "list sessions",
		Long:    "List sessions, newest state first. -wide adds start and end times and the resume id.",
		Example: "rex ls -state needs_input", Class: ClassQuery},
	{Name: "status", Section: SectionWatch, Args: "[flags]", Brief: "", Short: "counts; exit 1 if one needs input",
		Long:    "Print the number of sessions per state. Exits 1 when a session needs input.",
		Example: "rex status", Class: ClassQuery},
	{Name: "log", Section: SectionWatch, Args: "<sel> [flags]", Brief: "<sel> [-f] [-n N]", Short: "print a session's transcript",
		Long:    "Print the session transcript with ANSI stripped; -bytes keeps the raw bytes.",
		Example: "rex log fix-auth -n 50", Class: ClassContent},
	{Name: "overview", Section: SectionWatch, Args: "[flags]", Brief: "", Short: "one-screen summary by bucket",
		Example: "rex overview", Class: ClassQuery},
	{Name: "usage", Section: SectionWatch, Args: "[flags]", Brief: "[-range R] [-by model]", Short: "tokens, cost and plan quota",
		Long:    "Show token use, cost and plan quota per agent and model. -range limits the window, -tool filters, -by groups.",
		Example: "rex usage -range 7d -by model", Class: ClassQuery},
	{Name: "digest", Section: SectionWatch, Args: "[flags]", Brief: "", Short: "today's sessions, time and totals",
		Example: "rex digest -since 48h", Class: ClassQuery},
	{Name: "stats", Section: SectionWatch, Args: "[flags]", Brief: "", Short: "lifetime usage by model and tool",
		Example: "rex stats -all", Class: ClassQuery},
	{Name: "audit", Section: SectionWatch, Args: "[flags]", Brief: "[-since D] [-all]", Short: "one line per finished session",
		Long:    "List finished sessions from the audit trail: the global one, or a workspace's with --ws <id|path>. --all reads both. --since, --tool and --state filter.",
		Example: "rex audit --since 7d --state failed", Class: ClassQuery},
	{Name: "errors", Section: SectionWatch, Args: "[-n N] [-since 24h] [-clear]", Brief: "[-n N] [-clear]", Short: "failed rex commands",
		Long:    "Show the private log of failed rex commands, newest first. -clear deletes it. Turn the log off with cli_error_log or REX_ERROR_LOG=0.",
		Example: "rex errors -n 5", Class: ClassQuery},

	{Name: "name", Section: SectionManage, Args: "<sel> [-clear] [-- <text>]", Brief: "<sel> [-- <text>]", Short: "set, show or clear the display name",
		Long:    "Set the name the board shows, print it when there is no text, or clear it (the board then shows the slug).",
		Example: "rex name fix-auth -- Auth fix", Class: ClassAction},
	{Name: "rename", Section: SectionManage, Args: "<sel> <slug>", Brief: "<sel> <slug>", Short: "change a session's slug",
		Example: "rex rename 7d4f fix-auth", Class: ClassAction},
	{Name: "done", Aliases: []string{"retire"}, Section: SectionManage, Args: "<sel> [-after 72h]", Brief: "<sel> [-after 72h]", Short: "mark done; cleared after retire_after_hours",
		Long:    "Mark a session done without stopping it. After -after (default retire_after_hours) the daemon removes it.",
		Example: "rex done fix-auth -after 24h", Class: ClassAction},
	{Name: "undone", Aliases: []string{"unretire"}, Section: SectionManage, Args: "<sel>", Brief: "<sel>", Short: "cancel a pending done",
		Example: "rex undone fix-auth", Class: ClassAction},
	{Name: "complete", Section: SectionManage, Args: "<sel>", Brief: "<sel>", Short: "stop a session now and mark it done",
		Example: "rex complete fix-auth", Class: ClassAction},
	{Name: "resume", Section: SectionManage, Args: "<sel> [-print]", Brief: "<sel> [-print]", Short: "respawn an exited session",
		Long:    "Respawn an exited session in place, or unmark a running one marked done. -print prints the full resume id and sends nothing.",
		Example: "rex resume fix-auth", Class: ClassAction},
	{Name: "undo", Section: SectionManage, Args: "<sel> [-turns N] [-yes]", Brief: "<sel> [-turns N]", Short: "restore the folder to before the last turn",
		Long:    "Restore the session's folder to its state before the last agent turn (needs turn_snapshots). Asks first; without a terminal it needs -yes.",
		Example: "rex undo fix-auth -turns 1", Class: ClassAction},
	{Name: "redo", Section: SectionManage, Args: "<sel> [-yes]", Brief: "<sel>", Short: "bring back what undo replaced",
		Example: "rex redo fix-auth", Class: ClassAction},
	{Name: "archive", Section: SectionManage, Args: "<sel>", Brief: "<sel>", Short: "archive a session",
		Example: "rex archive fix-auth", Class: ClassAction},
	{Name: "rm", Section: SectionManage, Args: "<sel> [-force]", Brief: "<sel> [-force]", Short: "delete a session",
		Long:    "Delete a session and its files. Asks first; without a terminal it needs -force.",
		Example: "rex rm fix-auth -force", Class: ClassAction},
	{Name: "prune", Section: SectionManage, Args: "[-dry-run] [-yes]", Brief: "[-dry-run] [-yes]", Short: "delete Completed sessions",
		Long:    "Delete every session in the Completed column, keeping any still working. Asks first; without a terminal it needs -yes.",
		Example: "rex prune -dry-run", Class: ClassAction},
	{Name: "fleet", Section: SectionManage, Args: "ls|show|set|unset", Brief: "ls|show|set|unset", Short: "list or edit session fleets",
		Example: "rex fleet ls", Class: ClassQuery},
	{Name: "export", Section: SectionManage, Args: "<sel> [flags]", Brief: "<sel>", Short: "write a session as a readable document",
		Long:    "Write one session's metadata and cleaned transcript as json, yaml, md or txt to stdout or --out. Nothing is uploaded.",
		Example: "rex export fix-auth -o md --out fix-auth.md", Class: ClassContent},

	{Name: "daemon", Section: SectionDaemon, Args: "start|stop|restart|status|logs", Brief: "start|stop|restart|…", Short: "manage rex-daemon (also status, logs)",
		Long:    "Start, stop, restart or check the background daemon, or show its log. restart is a hot restart that keeps running sessions (-cold: stop and start, asks first when sessions would die). status exits 4 when it is not running.",
		Example: "rex daemon status", Class: ClassAction},
	{Name: "reload", Section: SectionDaemon, Args: "[flags]", Brief: "", Short: "reload tools.yaml, config and Lua",
		Long:    "Re-read every layer that applies live and print what changed, what needs a restart, and errors with file:line.",
		Example: "rex reload", Class: ClassAction},
	{Name: "upgrade", Section: SectionDaemon, Args: "[-cold] [-yes] [flags]", Brief: "[-cold] [-yes]", Short: "hot-restart onto a newly installed build",
		Long:    "After make install, make update or rex update: hot-restart the daemon (running sessions survive), then have every board re-exec into the new binary. Reports each process. -if-stale skips a daemon already on the installed build; -no-cold refuses instead of a cold restart.",
		Example: "rex upgrade", Class: ClassAction},

	{Name: "setup", Section: SectionSetup, Args: "[flags]", Brief: "[-yes]", Short: "first-run wizard",
		Long:    "Pick a default tool, model and effort and install the shell profile block. -yes accepts the defaults; -preview shows what it would do.",
		Example: "rex setup -yes", Class: ClassInteractive},
	{Name: "onboard", Section: SectionSetup, Args: "[flags]", Brief: "", Short: "guided tour: checks, agents, keys",
		Long:    "Check the install, list the coding agents on PATH (with install hints for the rest), run rex setup when there is no config, then walk through the board keys. -check runs the checks alone, in any format.",
		Example: "rex onboard -check", Class: ClassInteractive},
	{Name: "doctor", Section: SectionSetup, Args: "[flags]", Brief: "", Short: "check the install; exit 1 if a check fails",
		Example: "rex doctor -verbose", Class: ClassQuery},
	{Name: "config", Section: SectionSetup, Args: "list|get|set|reset|edit", Brief: "list|get|set|reset|…", Short: "view and change settings",
		Long:    "List, read and change settings (config.yaml; list and get show where each value comes from). edit opens init.lua, edit -lua opens config.lua (settings in Lua). lua-export prints config.yaml's values as a starter config.lua. Changing a trust setting (skip_permissions, auto_trust_dirs) works only at a terminal (exit 6 otherwise).",
		Example: "rex config set spinner moon", Class: ClassQuery},
	{Name: "theme", Section: SectionSetup, Args: "list|show|new", Brief: "list|show|new", Short: "list, inspect and create color themes",
		Long:    "List the built-in and user themes, show one theme's colors and glyphs, or write a commented theme file to edit (~/.config/rex/themes). Apply one with rex config set theme <name>.",
		Example: "rex theme new my-theme -from noir", Class: ClassQuery},
	{Name: "trust", Section: SectionSetup, Args: "[dir] [-ls] [-rm <dir>]", Brief: "[dir] [-ls] [-rm dir]", Short: "trust a folder so agents skip its dialog",
		Long:    "Add a folder to auto_trust_dirs (default: the repo root of the working directory, or the folder itself) and print the exact folder added. -ls lists trusted folders, -rm removes one. The home folder, its ancestors and relative paths are refused. For one session only, use rex new -trust.",
		Example: "rex trust ~/dev/api", Class: ClassAction},
	{Name: "ws", Section: SectionSetup, Args: "init|id|ls|rename|color|trust|open", Brief: "init|ls|open|…", Short: "workspaces (needs workspaces)",
		Long:    "Mark a project folder as a workspace with a .rex/ identity (id, name, color), show it, list the known ones, rename or recolor, trust its .rex/config.lua (trust [-rm]), or open the board scoped to one (open [path], like rex here). Needs workspaces.",
		Example: "rex ws init --name api", Class: ClassAction},
	{Name: "skills", Section: SectionSetup, Args: "[flags]", Brief: "", Short: "list the skills library",
		Example: "rex skills -o json", Class: ClassQuery},
	{Name: "update", Section: SectionSetup, Args: "[flags]", Brief: "[-check]", Short: "upgrade rex in place",
		Example: "rex update -check", Class: ClassAction},
	{Name: "uninstall", Section: SectionSetup, Args: "[flags]", Brief: "[-all] [-dry-run]", Short: "remove rex and optional state",
		Long:    "Remove the binaries and the profile block; -wipe-state and -purge-config also remove data. -dry-run shows the plan.",
		Example: "rex uninstall -dry-run", Class: ClassAction},
	{Name: "completion", Section: SectionSetup, Args: "bash|zsh|fish", Brief: "bash|zsh|fish", Short: "print a shell completion script",
		Example: "source <(rex completion zsh)", Class: ClassContent},
	{Name: "render", Section: SectionSetup, Args: "[flags]", Brief: "", Short: "render an event stream from stdin",
		Example: "rex render -w 80 -h 24", Class: ClassContent},
	{Name: "version", Section: SectionSetup, Args: "", Brief: "", Short: "print the version",
		Example: "rex version", Class: ClassQuery},
	{Name: "agent-help", Section: SectionSetup, Args: "", Brief: "", Short: "the usage card for LLM agents",
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
