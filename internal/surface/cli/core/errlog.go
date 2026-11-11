package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/runtime/redact"
	"github.com/tristanbietsch/rex/internal/runtime/rexlog"
)

// The CLI error log: one JSONL record per failed command in
// ~/.local/state/rex/cli-errors.log (0600 in a 0700 folder), so an error is
// still there after the terminal scrolls. A record holds the command name,
// the flags (values redacted), the exit code and kind, and scrubbed error
// text. It never holds prompts, reply or send text, config values, working
// folders, socket paths, stdin, the environment or transcripts.
const (
	// errLogName is the log's name for rexlog.Path.
	errLogName = "cli-errors"
	// errLogMaxBytes is the size at which the log rotates to the backup.
	errLogMaxBytes = 1 << 20
	// errLogMaxRecord caps one record, so a single write stays atomic.
	errLogMaxRecord = 4096
	// errLogMaxMessage caps one error message, in runes.
	errLogMaxMessage = 512
	// errLogMaxChain caps the wrapped-error chain.
	errLogMaxChain = 8
)

// ErrLogRecord is one line of the error log.
type ErrLogRecord struct {
	V         int       `json:"v"`
	Time      string    `json:"time"`
	Version   string    `json:"version"`
	OS        string    `json:"os"`
	PID       int       `json:"pid"`
	Cmd       string    `json:"cmd"`
	Args      []string  `json:"args"`
	Format    string    `json:"format"`
	AgentAuto bool      `json:"agent_auto"`
	TTY       ErrLogTTY `json:"tty"`
	Exit      int       `json:"exit"`
	Kind      string    `json:"kind"`
	Error     string    `json:"error"`
	Chain     []string  `json:"chain,omitempty"`
	Hint      string    `json:"hint,omitempty"`
}

// ErrLogTTY says which standard streams were terminals.
type ErrLogTTY struct {
	In  bool `json:"in"`
	Out bool `json:"out"`
}

// secretPatterns are scrubbed from error text before it is written: defense
// in depth on top of the allowlist that keeps values out of the arguments.
var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`sk-[A-Za-z0-9_-]{8,}`),
	regexp.MustCompile(`(?:ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{8,}`),
	regexp.MustCompile(`github_pat_[A-Za-z0-9_]{8,}`),
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
	regexp.MustCompile(`xox[abps]-[A-Za-z0-9-]{8,}`),
	regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._~+/=-]{8,}`),
	regexp.MustCompile(`(?i)\b(password|passwd|token|secret|api[_-]?key)=\S+`),
}

// Scrub replaces secrets in s with <redacted>.
func Scrub(s string) string {
	for _, re := range secretPatterns {
		s = re.ReplaceAllStringFunc(s, func(m string) string {
			if i := strings.Index(m, "="); i > 0 && !strings.HasPrefix(m, "sk-") {
				return m[:i+1] + "<redacted>"
			}
			return "<redacted>"
		})
	}
	return s
}

// logMessage makes an error message loggable: one line, scrubbed, capped.
func logMessage(s string) string {
	s = Scrub(flat(s))
	if utf8.RuneCountInString(s) > errLogMaxMessage {
		r := []rune(s)
		s = string(r[:errLogMaxMessage-1]) + "…"
	}
	return s
}

// errChain lists the causes under err (not err itself), walking Unwrap and
// Unwrap() []error, up to errLogMaxChain messages.
func errChain(err error) []string {
	var out []string
	var visit func(e error)
	visit = func(e error) {
		for e != nil && len(out) < errLogMaxChain {
			out = append(out, logMessage(e.Error()))
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
	switch u := err.(type) { //nolint:errorlint // see above.
	case interface{ Unwrap() []error }:
		for _, c := range u.Unwrap() {
			visit(c)
		}
	case interface{ Unwrap() error }:
		visit(u.Unwrap())
	}
	return out
}

// cliKeepValues are the flags whose values are safe to log.
var cliKeepValues = map[string]bool{
	"tool": true, "model": true, "effort": true, "o": true, "output": true,
	"until": true, "timeout": true, "state": true, "after": true, "n": true,
	"since": true, "date": true,
}

// cliPolicy is the redaction policy for a command's arguments: the verb and
// flag names stay, allowlisted flag values stay, selectors become sizes (or
// stay when a short hex id), and free text is replaced.
func cliPolicy(verb string, rest []string) redact.Policy {
	p := redact.Policy{KeepValues: cliKeepValues}
	switch verb {
	case "new":
		p.FreeTextFrom = 2
	case "reply", "send", "name":
		p.Selectors, p.FreeTextFrom = 1, 3
	case "config":
		if len(rest) > 0 && rest[0] == "set" {
			p.FreeTextFrom = 4
		}
	case "attach", "wait", "rm", "rename", "done", "retire", "undone", "unretire", "archive", "resume", "complete", "log":
		p.Selectors = 1
	}
	return p
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

// RecordError appends err to the error log. It records nothing for a help
// request or a bare exit status (status found a session needing input,
// doctor found a failure), and never changes the exit code or prints, except
// one stderr line when REX_LOG_LEVEL=debug.
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

func buildRecord(args []string, err error, now time.Time) ErrLogRecord {
	info := Describe(err)
	p := Out()
	rest, _, _ := stripGlobalFlags(args)
	verb := "rex"
	var after []string
	if len(rest) > 0 {
		verb, after = rest[0], rest[1:]
	}
	format := p.Format.String()
	if p.Legacy {
		format = "json-legacy"
	}
	rec := ErrLogRecord{
		V: 1, Time: now.UTC().Format("2006-01-02T15:04:05.000Z"),
		Version: Version, OS: runtime.GOOS + "/" + runtime.GOARCH, PID: os.Getpid(),
		Cmd: verb, Args: redact.Argv(append([]string{"rex"}, args...), cliPolicy(verb, after))[1:],
		Format: format, AgentAuto: p.AgentAuto, TTY: ErrLogTTY{In: p.InTTY, Out: p.OutTTY},
		Exit: info.Code, Kind: info.Kind, Error: logMessage(info.Msg), Chain: errChain(err), Hint: logMessage(info.Hint),
	}
	if rec.Args == nil {
		rec.Args = []string{}
	}
	return rec
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

// shrinkRecord drops the chain, then trims the error text, until the record
// fits errLogMaxRecord.
func shrinkRecord(record []byte) []byte {
	var rec ErrLogRecord
	if json.Unmarshal(record, &rec) != nil {
		return record[:errLogMaxRecord]
	}
	rec.Chain = nil
	for {
		b, err := json.Marshal(rec)
		if err != nil || len(b) <= errLogMaxRecord {
			return b
		}
		r := []rune(rec.Error)
		if len(r) < 16 {
			rec.Args, rec.Hint = nil, ""
			rec.Error = string(r)
			b, _ = json.Marshal(rec)
			if len(b) > errLogMaxRecord {
				b = b[:errLogMaxRecord]
			}
			return b
		}
		rec.Error = string(r[:len(r)/2]) + "…"
	}
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
