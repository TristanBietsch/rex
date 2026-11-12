package core

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/runtime/rexlog"
)

// The CLI error log: one JSONL record per failed command in
// ~/.local/state/rex/cli-errors.log (0600 in a 0700 folder), so a failure is
// still known after the terminal scrolls.
//
// It records STRUCTURE ONLY. Nothing a person typed and no error message
// ever reaches it: free text cannot be reliably scrubbed, so none is kept.
// A record holds the command and subcommand when rex knows them, the NAMES
// of the flags that command defines, how many other arguments there were, the
// output format, the terminal state, the exit code and kind, the Go types of
// the error and its causes, and a stable code from a fixed list. No values,
// no selectors, no paths, no prompts, no message or hint text.
const (
	// errLogName is the log's name for rexlog.Path.
	errLogName = "cli-errors"
	// errLogMaxBytes is the size at which the log rotates to the backup.
	errLogMaxBytes = 1 << 20
	// errLogMaxRecord caps one record, so a single write stays atomic.
	errLogMaxRecord = 4096
	// errLogMaxChain caps the wrapped-error type chain.
	errLogMaxChain = 8
	// errLogMaxFlags caps the flag names in one record.
	errLogMaxFlags = 32
)

// ErrLogRecord is one line of the error log.
type ErrLogRecord struct {
	V       int    `json:"v"`
	Time    string `json:"time"`
	Version string `json:"version"`
	OS      string `json:"os"`
	PID     int    `json:"pid"`
	// Cmd is the command, "?" when rex does not know the name.
	Cmd string `json:"cmd"`
	// Sub is the subcommand (fleet set, config get), only when known.
	Sub string `json:"sub,omitempty"`
	// Flags are names (with their dashes) of flags the command defines.
	Flags []string `json:"flags"`
	// ArgCount is the number of arguments after the global output flags;
	// UnknownArgs counts the dash arguments that name no flag.
	ArgCount    int       `json:"arg_count"`
	UnknownArgs int       `json:"unknown_args"`
	Format      string    `json:"format"`
	AgentAuto   bool      `json:"agent_auto"`
	TTY         ErrLogTTY `json:"tty"`
	Exit        int       `json:"exit"`
	Kind        string    `json:"kind"`
	// Code is a stable error name from errCodes, when the error has one.
	Code string `json:"code,omitempty"`
	// Chain is the Go type of the error and of each cause under it.
	Chain []string `json:"chain,omitempty"`
}

// ErrLogTTY says which standard streams were terminals.
type ErrLogTTY struct {
	In  bool `json:"in"`
	Out bool `json:"out"`
}

// errCodes is the fixed list of ExitError.ID values the log keeps; anything
// else is dropped.
var errCodes = map[string]bool{
	"daemon_unreachable": true, "handshake_failed": true,
	"selector_not_found": true, "selector_ambiguous": true,
	"unknown_command": true, "invalid_flag": true,
	"feature_off": true, "needs_confirmation": true,
}

// cliSubcommands are the subcommands rex knows, per command.
var cliSubcommands = map[string][]string{
	"fleet":  {"ls", "show", "set", "unset"},
	"daemon": {"start", "stop", "status", "restart", "logs"},
	"config": {"list", "get", "set", "reset", "edit"},
}

// globalFlagNames are the output flags every command takes.
var globalFlagNames = []string{"o", "output", "agent", "no-color", "json", "h", "help"}

var (
	registeredMu    sync.Mutex
	registeredFlags = map[string]bool{}
)

// noteRegisteredFlags remembers the flag names the command being parsed
// defines. The error log reports only flags that name one of these, so a
// typed prompt that starts with a dash is never mistaken for a flag.
func noteRegisteredFlags(fs *flag.FlagSet) {
	registeredMu.Lock()
	defer registeredMu.Unlock()
	fs.VisitAll(func(f *flag.Flag) { registeredFlags[f.Name] = true })
}

func flagIsRegistered(name string) bool {
	registeredMu.Lock()
	defer registeredMu.Unlock()
	return registeredFlags[name] || slices.Contains(globalFlagNames, name)
}

// errChain lists the Go type of err and of each cause under it, walking
// Unwrap and Unwrap() []error, up to errLogMaxChain entries.
func errChain(err error) []string {
	var out []string
	var visit func(e error)
	visit = func(e error) {
		for e != nil && len(out) < errLogMaxChain {
			out = append(out, fmt.Sprintf("%T", e))
			switch u := e.(type) { //nolint:errorlint // walking the chain by hand is the point.
			case interface{ Unwrap() []error }:
				for _, c := range u.Unwrap() {
					visit(c)
				}
				return
			case interface{ Unwrap() error }:
				e = u.Unwrap()
			default:
				return
			}
		}
	}
	visit(err)
	return out
}

// ErrLogOptions are the inputs RecordError cannot read from the process, so
// tests can pin them.
type ErrLogOptions struct {
	// Path overrides the log path (default rexlog.Path("cli-errors")).
	Path string
	Now  func() time.Time
	// Enabled overrides the opt-out checks.
	Enabled func() bool
}

// errLogEnabled reports whether failed commands are recorded: REX_ERROR_LOG=0
// and the cli_error_log setting both opt out. A settings file that will not
// load means on.
func errLogEnabled() bool {
	if os.Getenv("REX_ERROR_LOG") == "0" {
		return false
	}
	store, _, err := settings.LoadDefault()
	if err != nil {
		return true
	}
	on, ok := store.Get("cli_error_log").(bool)
	return !ok || on
}

// RecordError appends the structure of a failed command to the error log. It
// records nothing for a help request or a bare exit status (status found a
// session needing input, doctor found a failure), and never changes the exit
// code or prints, except one stderr line when REX_LOG_LEVEL=debug.
func RecordError(args []string, err error, opts ErrLogOptions) {
	if IsSilent(err) {
		return
	}
	enabled := opts.Enabled
	if enabled == nil {
		enabled = errLogEnabled
	}
	if !enabled() {
		return
	}
	if werr := writeErrLog(args, err, opts); werr != nil && strings.EqualFold(os.Getenv("REX_LOG_LEVEL"), "debug") {
		fmt.Fprintf(Out().Err, "rex: error log: %v\n", werr)
	}
}

func writeErrLog(args []string, err error, opts ErrLogOptions) error {
	path := opts.Path
	if path == "" {
		var perr error
		if path, perr = rexlog.Path(errLogName); perr != nil {
			return perr
		}
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	data, merr := json.Marshal(buildRecord(args, err, now()))
	if merr != nil {
		return merr
	}
	return appendErrLog(path, data)
}

// buildRecord reduces a failed command to its structure.
func buildRecord(args []string, err error, now time.Time) ErrLogRecord {
	info := Describe(err)
	p := Out()
	rest, _, _ := stripGlobalFlags(args)

	cmd, sub := "?", ""
	if len(rest) > 0 {
		if _, known := FindCommand(rest[0]); known || rest[0] == "help" {
			cmd = rest[0]
			if len(rest) > 1 {
				sub = knownSubcommand(cmd, rest[1])
			}
		}
	}

	// Flags: names only, and only names the command (or the global output
	// flags) defines. A dash argument that names nothing is just counted.
	flags, unknown := []string{}, 0
	for _, a := range args {
		if a == "--" {
			break
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			continue
		}
		name, _, _ := strings.Cut(strings.TrimLeft(a, "-"), "=")
		switch {
		case cmd != "?" && flagIsRegistered(name):
			f := "-" + name
			if !slices.Contains(flags, f) && len(flags) < errLogMaxFlags {
				flags = append(flags, f)
			}
		default:
			unknown++
		}
	}

	format := p.Format.String()
	if p.Legacy {
		format = "json-legacy"
	}
	rec := ErrLogRecord{
		V: 1, Time: now.UTC().Format("2006-01-02T15:04:05.000Z"),
		Version: Version, OS: runtime.GOOS + "/" + runtime.GOARCH, PID: os.Getpid(),
		Cmd: cmd, Sub: sub, Flags: flags, ArgCount: len(rest), UnknownArgs: unknown,
		Format: format, AgentAuto: p.AgentAuto, TTY: ErrLogTTY{In: p.InTTY, Out: p.OutTTY},
		Exit: info.Code, Kind: info.Kind, Chain: errChain(err),
	}
	var ee ExitError
	if errors.As(err, &ee) && errCodes[ee.ID] {
		rec.Code = ee.ID
	}
	return rec
}

// knownSubcommand returns word when it is a subcommand rex has for cmd.
func knownSubcommand(cmd, word string) string {
	if slices.Contains(cliSubcommands[cmd], word) {
		return word
	}
	return ""
}

// appendErrLog writes one record, rotating first when the log is full. The
// record is one Write on an O_APPEND file, so concurrent rex processes
// interleave whole lines; a rotation race can lose a few lines, which is
// acceptable for a diagnostic log.
func appendErrLog(path string, record []byte) error {
	if len(record) > errLogMaxRecord {
		record = shrinkRecord(record)
	}
	record = append(record, '\n')
	if err := ensureLogDir(path); err != nil {
		return err
	}
	if fi, err := os.Stat(path); err == nil && fi.Size() >= errLogMaxBytes {
		_ = os.Remove(path + ".1") // Rename onto an existing file fails on Windows
		if err := os.Rename(path, path+".1"); err != nil {
			return err
		}
	}
	f, err := rexlog.OpenPrivate(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(record)
	return err
}

// ensureLogDir creates the log's folder 0700. rex's own folder is tightened
// to 0700; a REX_LOG_DIR the user chose may be shared and is never chmodded.
func ensureLogDir(path string) error {
	dir := filepath.Dir(path)
	if os.Getenv("REX_LOG_DIR") != "" {
		return os.MkdirAll(dir, 0o700)
	}
	return rexlog.PrivateDir(dir)
}

// shrinkRecord drops the chain, then the flag names, until the record fits
// errLogMaxRecord. Every field is bounded, so this is a backstop.
func shrinkRecord(record []byte) []byte {
	var rec ErrLogRecord
	if json.Unmarshal(record, &rec) != nil {
		return record[:errLogMaxRecord]
	}
	rec.Chain = nil
	if b, err := json.Marshal(rec); err == nil && len(b) <= errLogMaxRecord {
		return b
	}
	rec.Flags = []string{}
	b, err := json.Marshal(rec)
	if err != nil || len(b) > errLogMaxRecord {
		return record[:errLogMaxRecord]
	}
	return b
}

// ReadErrLog returns the records of the log and its backup, newest first.
// Lines that are not valid records are skipped.
func ReadErrLog(path string) ([]ErrLogRecord, error) {
	var out []ErrLogRecord
	for _, p := range []string{path, path + ".1"} {
		b, err := os.ReadFile(p)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		lines := strings.Split(string(b), "\n")
		for i := len(lines) - 1; i >= 0; i-- {
			var rec ErrLogRecord
			if lines[i] != "" && json.Unmarshal([]byte(lines[i]), &rec) == nil && rec.V == 1 {
				out = append(out, rec)
			}
		}
	}
	return out, nil
}

// ErrLogPath is where the error log lives.
func ErrLogPath() (string, error) { return rexlog.Path(errLogName) }

// ClearErrLog removes the log and its backup.
func ClearErrLog(path string) error {
	for _, p := range []string{path, path + ".1"} {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}
