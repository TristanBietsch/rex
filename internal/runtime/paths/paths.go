// Package paths is the single source of truth for where rex keeps its files.
//
// Every rex path derives from the user's home directory, plus
// $XDG_RUNTIME_DIR for the socket. The layout, with ~ the home directory:
//
//	~/.config/rex/                 ConfigDir: files the user edits
//	    config.yaml                ConfigFile: settings (rex setup, rex config, TUI)
//	    tools.yaml                 ToolsFile: tool registry overlay
//	    init.lua                   LuaInitFile: optional Lua hooks
//	    skills/<name>/SKILL.md     SkillsDir: skill library (rex new -skill)
//	    loops/<name>.yaml          LoopsDir: loop workflow files (loops feature)
//	~/.local/share/rex/            DataDir: durable session data (rex-daemon -state-dir)
//	    daemon.lock                DaemonLock: one daemon per DataDir
//	    usage.jsonl                UsageLedger: per-session tokens and cost (usage feature)
//	    workspaces.json            WorkspacesFile: registry of workspace ids and folders (workspaces feature)
//	    audit.jsonl                AuditFile: one line per finished session outside a workspace (audit_trail)
//	    sessions/<id>/             SessionsDir: meta.json, transcript.log, hook file
//	    snapshots/<key>/           SnapshotsDir: turn-snapshot shadow stores, 0700
//	    exports/                   ExportsDir: files :export writes (0600)
//	    loops/<id>.json, <id>.log  LoopStateDir: loop state and log (0600)
//	~/.local/state/rex/            LogDir: logs and ephemeral TUI state
//	    daemon.log                 DaemonLog: rex-daemon stderr (rex daemon start)
//	    <name>.log                 rexlog files (daemon, tui, setup, ...)
//	    cli-errors.log             CLIErrorLog: failed rex commands (+ .1 backup)
//	    tui-state.json             TUIStateFile: board selection kept across :bg
//	~/.cache/rex/                  CacheDir
//	    rex.sock                   Socket, when $XDG_RUNTIME_DIR is unset
//	$XDG_RUNTIME_DIR/rex.sock      Socket, when $XDG_RUNTIME_DIR is set
//
// DataDir and LogDir are different trees: "share" holds what must survive,
// "state" holds logs and what may be lost. The other XDG base-directory
// variables (XDG_CONFIG_HOME, XDG_DATA_HOME, ...) are not consulted; the
// locations above are where every rex release has kept its files, and
// honoring them now would strand existing data. REX_LOG_DIR moves only the
// rexlog files (see package rexlog), never daemon.log or tui-state.json.
//
// A home directory that cannot be determined, or is not absolute, is an
// error (ErrNoHome). There is no fallback: a relative or shared fallback
// such as the working directory or /tmp would let unrelated processes or
// users read and write rex's state.
//
// Every function reads the environment on each call, so it is safe for
// concurrent use and reflects t.Setenv in tests. None of them creates,
// stats or chmods anything; callers create directories with the modes they
// need.
package paths

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// File and directory names inside the layout. They are part of the on-disk
// contract: changing one strands existing user data.
const (
	appDir           = "rex"
	configFileName   = "config.yaml"
	toolsFileName    = "tools.yaml"
	luaInitFileName  = "init.lua"
	skillsDirName    = "skills"
	loopsDirName     = "loops"
	sessionsDirName  = "sessions"
	snapshotsDirName = "snapshots"
	exportsDirName   = "exports"
	daemonLockName   = "daemon.lock"
	usageLedgerName  = "usage.jsonl"
	workspacesName   = "workspaces.json"
	auditFileName    = "audit.jsonl"
	daemonLogName    = "daemon.log"
	tuiStateFileName = "tui-state.json"
	socketName       = "rex.sock"
)

// ErrNoHome reports that the home directory is unset, empty or not an
// absolute path. Every accessor except Socket (with $XDG_RUNTIME_DIR set)
// fails with an error wrapping it then; match with errors.Is.
var ErrNoHome = errors.New("cannot determine the home directory (set HOME)")

// Layout is every home-derived rex path for one home directory. Build it
// with ForHome, or Resolve for the current user. The socket is not part of
// it because it also depends on $XDG_RUNTIME_DIR; see Socket.
type Layout struct {
	Home           string // the home directory the layout derives from
	ConfigDir      string // ~/.config/rex
	ConfigFile     string // ~/.config/rex/config.yaml
	ToolsFile      string // ~/.config/rex/tools.yaml
	LuaInitFile    string // ~/.config/rex/init.lua
	SkillsDir      string // ~/.config/rex/skills
	LoopsDir       string // ~/.config/rex/loops
	LoopStateDir   string // ~/.local/share/rex/loops
	DataDir        string // ~/.local/share/rex
	SessionsDir    string // ~/.local/share/rex/sessions
	SnapshotsDir   string // ~/.local/share/rex/snapshots
	ExportsDir     string // ~/.local/share/rex/exports
	DaemonLock     string // ~/.local/share/rex/daemon.lock
	UsageLedger    string // ~/.local/share/rex/usage.jsonl
	WorkspacesFile string // ~/.local/share/rex/workspaces.json
	AuditFile      string // ~/.local/share/rex/audit.jsonl
	LogDir         string // ~/.local/state/rex
	DaemonLog      string // ~/.local/state/rex/daemon.log
	TUIStateFile   string // ~/.local/state/rex/tui-state.json
	CacheDir       string // ~/.cache/rex
}

// ForHome returns the layout under home. It is pure: it neither reads the
// environment nor validates home. Use it to compute the paths for a home
// other than the current user's (tests, uninstall plans); use Resolve for
// the current user.
func ForHome(home string) Layout {
	config := filepath.Join(home, ".config", appDir)
	data := filepath.Join(home, ".local", "share", appDir)
	logs := filepath.Join(home, ".local", "state", appDir)
	return Layout{
		Home:           home,
		ConfigDir:      config,
		ConfigFile:     filepath.Join(config, configFileName),
		ToolsFile:      filepath.Join(config, toolsFileName),
		LuaInitFile:    filepath.Join(config, luaInitFileName),
		SkillsDir:      filepath.Join(config, skillsDirName),
		LoopsDir:       filepath.Join(config, loopsDirName),
		LoopStateDir:   filepath.Join(data, loopsDirName),
		DataDir:        data,
		SessionsDir:    filepath.Join(data, sessionsDirName),
		SnapshotsDir:   filepath.Join(data, snapshotsDirName),
		ExportsDir:     filepath.Join(data, exportsDirName),
		DaemonLock:     filepath.Join(data, daemonLockName),
		UsageLedger:    filepath.Join(data, usageLedgerName),
		WorkspacesFile: filepath.Join(data, workspacesName),
		AuditFile:      filepath.Join(data, auditFileName),
		LogDir:         logs,
		DaemonLog:      filepath.Join(logs, daemonLogName),
		TUIStateFile:   filepath.Join(logs, tuiStateFileName),
		CacheDir:       filepath.Join(home, ".cache", appDir),
	}
}

// Resolve returns the layout for the current user's home directory, or an
// error wrapping ErrNoHome.
func Resolve() (Layout, error) {
	home, err := Home()
	if err != nil {
		return Layout{}, err
	}
	return ForHome(home), nil
}

// Home returns the current user's home directory: $HOME on Unix,
// %USERPROFILE% on Windows (os.UserHomeDir). It fails with an error wrapping
// ErrNoHome when that is unset, empty or relative.
func Home() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrNoHome, err)
	}
	if !filepath.IsAbs(home) {
		return "", fmt.Errorf("%w: %q is not an absolute path", ErrNoHome, home)
	}
	return home, nil
}

// ExpandHome replaces a leading "~/" (or "~\" on Windows) in p with the
// home directory. Any other p is returned unchanged without looking the
// home directory up, so only a "~/" path can fail.
func ExpandHome(p string) (string, error) {
	if !IsHomeRelative(p) {
		return p, nil
	}
	home, err := Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, p[2:]), nil
}

// IsHomeRelative reports whether p starts with "~/" (or "~\" on Windows),
// the only form ExpandHome expands.
func IsHomeRelative(p string) bool {
	return len(p) >= 2 && p[0] == '~' && os.IsPathSeparator(p[1])
}

// field resolves the current layout and returns one of its paths.
func field(get func(Layout) string) (string, error) {
	l, err := Resolve()
	if err != nil {
		return "", err
	}
	return get(l), nil
}

// ConfigDir returns ~/.config/rex, the directory of files the user edits.
func ConfigDir() (string, error) { return field(func(l Layout) string { return l.ConfigDir }) }

// ConfigFile returns ~/.config/rex/config.yaml, the settings file.
func ConfigFile() (string, error) { return field(func(l Layout) string { return l.ConfigFile }) }

// ToolsFile returns ~/.config/rex/tools.yaml, the user's tool registry
// overlay (rex-daemon -tools default).
func ToolsFile() (string, error) { return field(func(l Layout) string { return l.ToolsFile }) }

// LuaInitFile returns ~/.config/rex/init.lua, the default Lua hooks file
// (the lua_config_path setting can point elsewhere).
func LuaInitFile() (string, error) { return field(func(l Layout) string { return l.LuaInitFile }) }

// SkillsDir returns ~/.config/rex/skills, the skill library: one folder per
// skill, each holding a SKILL.md (rex-daemon -skills-dir default).
func SkillsDir() (string, error) { return field(func(l Layout) string { return l.SkillsDir }) }

// LoopsDir returns ~/.config/rex/loops, where the user's loop workflow files
// (*.yaml) live. Only files the user owns are read from it.
func LoopsDir() (string, error) { return field(func(l Layout) string { return l.LoopsDir }) }

// LoopStateDir returns ~/.local/share/rex/loops, where the daemon keeps one
// <id>.json state file and one <id>.log per loop (mode 0600).
func LoopStateDir() (string, error) { return field(func(l Layout) string { return l.LoopStateDir }) }

// LoopStateDirName is the loop state folder's name inside a state directory,
// for a daemon started with a -state-dir other than DataDir.
const LoopStateDirName = loopsDirName

// DataDir returns ~/.local/share/rex, the durable session data root
// (rex-daemon -state-dir default).
func DataDir() (string, error) { return field(func(l Layout) string { return l.DataDir }) }

// SnapshotsDir returns ~/.local/share/rex/snapshots, the root of the turn
// snapshot shadow stores (one folder store per working folder, 0700).
func SnapshotsDir() (string, error) { return field(func(l Layout) string { return l.SnapshotsDir }) }

// SessionsDir returns ~/.local/share/rex/sessions, which holds one
// directory per session.
func SessionsDir() (string, error) { return field(func(l Layout) string { return l.SessionsDir }) }

// ExportsDir returns ~/.local/share/rex/exports, where the TUI's :export
// writes (one file per session, mode 0600). The caller creates it.
func ExportsDir() (string, error) { return field(func(l Layout) string { return l.ExportsDir }) }

// DaemonLock returns ~/.local/share/rex/daemon.lock, the lock that keeps a
// second daemon off the same DataDir.
func DaemonLock() (string, error) { return field(func(l Layout) string { return l.DaemonLock }) }

// UsageLedger returns ~/.local/share/rex/usage.jsonl, the append-only usage
// ledger (one JSON line per session snapshot, mode 0600).
func UsageLedger() (string, error) { return field(func(l Layout) string { return l.UsageLedger }) }

// UsageLedgerFileName is the ledger's file name inside a state directory,
// for a daemon started with a -state-dir other than DataDir.
const UsageLedgerFileName = usageLedgerName

// AuditFileName is the audit file's name inside a state directory, for a
// daemon started with a -state-dir other than DataDir.
const AuditFileName = auditFileName

// WorkspacesFile returns ~/.local/share/rex/workspaces.json, the registry of
// workspace ids and the folders they were last seen at.
func WorkspacesFile() (string, error) {
	return field(func(l Layout) string { return l.WorkspacesFile })
}

// AuditFile returns ~/.local/share/rex/audit.jsonl, the audit trail of
// sessions that ran outside any workspace (mode 0600).
func AuditFile() (string, error) { return field(func(l Layout) string { return l.AuditFile }) }

// LogDir returns ~/.local/state/rex, the directory of logs and ephemeral
// TUI state. It ignores REX_LOG_DIR, which only rexlog honors.
func LogDir() (string, error) { return field(func(l Layout) string { return l.LogDir }) }

// DaemonLog returns ~/.local/state/rex/daemon.log, where rex daemon start
// and the TUI send rex-daemon's stderr.
func DaemonLog() (string, error) { return field(func(l Layout) string { return l.DaemonLog }) }

// TUIStateFile returns ~/.local/state/rex/tui-state.json, the board
// selection and filter the TUI saves on :bg and reads once on next launch.
func TUIStateFile() (string, error) { return field(func(l Layout) string { return l.TUIStateFile }) }

// CacheDir returns ~/.cache/rex.
func CacheDir() (string, error) { return field(func(l Layout) string { return l.CacheDir }) }

// Socket returns the daemon's default Unix socket: $XDG_RUNTIME_DIR/rex.sock
// when XDG_RUNTIME_DIR is set to an absolute path, else
// ~/.cache/rex/rex.sock. A relative XDG_RUNTIME_DIR is ignored, as the XDG
// base-directory spec requires. rex-daemon and every client must agree on
// this path, so both resolve it here.
func Socket() (string, error) {
	if rt := os.Getenv("XDG_RUNTIME_DIR"); rt != "" && filepath.IsAbs(rt) {
		return filepath.Join(rt, socketName), nil
	}
	dir, err := CacheDir()
	if err != nil {
		return "", fmt.Errorf("default socket: %w", err)
	}
	return filepath.Join(dir, socketName), nil
}
