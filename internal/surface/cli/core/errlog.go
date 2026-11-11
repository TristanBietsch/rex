package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
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

// secretPattern is one shape of secret. keepKey patterns are key=value: the
// key stays and only the value goes.
type secretPattern struct {
	re      *regexp.Regexp
	keepKey bool
}

// secretPatterns are scrubbed from error text before it is written: defense
// in depth on top of the allowlist that keeps values out of the arguments.
var secretPatterns = []secretPattern{
	{regexp.MustCompile(`sk-[A-Za-z0-9_-]{8,}`), false},
	{regexp.MustCompile(`(?:ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{8,}`), false},
	{regexp.MustCompile(`github_pat_[A-Za-z0-9_]{8,}`), false},
	{regexp.MustCompile(`AKIA[0-9A-Z]{16}`), false},
	{regexp.MustCompile(`xox[abps]-[A-Za-z0-9-]{8,}`), false},
	{regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._~+/=-]{8,}`), false},
	{regexp.MustCompile(`(?i)\b(password|passwd|token|secret|api[_-]?key)=\S+`), true},
}

// Scrub replaces secrets in s with <redacted>.
func Scrub(s string) string {
	for _, sp := range secretPatterns {
		s = sp.re.ReplaceAllStringFunc(s, func(m string) string {
			if i := strings.Index(m, "="); sp.keepKey && i > 0 {
				return m[:i+1] + "<redacted>"
			}
			return "<redacted>"
		})
	}
	return s
}

// pathPattern finds absolute, ~/ and drive paths: they name folders, sockets
// and files of the user, which the log never keeps.
var pathPattern = regexp.MustCompile(`(^|[\s"'(=:\[])(?:[A-Za-z]:[\\/]|~[\\/]|/)[^\s"'()<>,;\]]*`)

// stripPaths replaces every path in s, and the home folder wherever it
// appears, with <path>.
func stripPaths(s string) string {
	if home, err := os.UserHomeDir(); err == nil && len(home) > 1 {
		s = strings.ReplaceAll(s, home, "<path>")
	}
	s = pathPattern.ReplaceAllString(s, "${1}<path>")
	return homeTail.ReplaceAllString(s, "<path>") // what followed the home folder
}

var homeTail = regexp.MustCompile(`<path>[\\/][^\s"'()<>,;\]]*`)

// stripValues replaces each value (an argument the redactor hid) found in s.
func stripValues(s string, values []string) string {
	for _, v := range values {
		s = strings.ReplaceAll(s, v, "<arg>")
	}
	return s
}

// logMessage makes an error message loggable: one line, without the
// argument values that were redacted and without paths, scrubbed, capped.
func logMessage(s string, hidden ...string) string {
	s = Scrub(stripPaths(stripValues(flat(s), hidden)))
	if utf8.RuneCountInString(s) > errLogMaxMessage {
		r := []rune(s)
		s = string(r[:errLogMaxMessage-1]) + "…"
	}
	return s
}

// errChain lists the Go types of the causes under err (not err itself),
// walking Unwrap and Unwrap() []error, up to errLogMaxChain entries. Only the
// kind is kept: the text of a dial or OS error carries paths and values.
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
	verb := "?" // a name rex does not know may be a prompt typed by mistake
	var after []string
	if len(rest) > 0 {
		if _, known := FindCommand(rest[0]); known || rest[0] == "help" {
			verb = rest[0]
		}
		after = rest[1:]
	}
	policy := cliPolicy(verb, after)
	if verb == "?" && len(rest) > 0 {
		policy = redact.Policy{FreeTextFrom: 1}
		policy.KeepValues = cliKeepValues
	}
	logArgs := redact.Argv(append([]string{"rex"}, args...), policy)[1:]
	hidden := hiddenValues(args, logArgs)
	if verb == "?" && len(rest) > 0 {
		hidden = append(hidden, rest[0])
		sortLongestFirst(hidden)
	}

	format := p.Format.String()
	if p.Legacy {
		format = "json-legacy"
	}
	text := info.Msg
	var ee ExitError
	if errors.As(err, &ee) && ee.Err != nil {
		// The wrapped cause (a dial or OS error) is in Msg too; its text
		// carries paths and values, so only the part before it is kept.
		text = strings.TrimRight(strings.TrimSuffix(text, ee.Err.Error()), ": ")
	}
	msg, chain := logMessage(text, hidden...), errChain(err)
	if verb == "config" && len(after) > 0 && after[0] == "set" {
		// The value being set is what failed; the text cannot be trusted to omit it.
		msg, chain = "config set: rejected ("+info.Kind+")", nil
	}
	rec := ErrLogRecord{
		V: 1, Time: now.UTC().Format("2006-01-02T15:04:05.000Z"),
		Version: Version, OS: runtime.GOOS + "/" + runtime.GOARCH, PID: os.Getpid(),
		Cmd: verb, Args: logArgs,
		Format: format, AgentAuto: p.AgentAuto, TTY: ErrLogTTY{In: p.InTTY, Out: p.OutTTY},
		Exit: info.Code, Kind: info.Kind, Error: msg, Chain: chain, Hint: logHint(info.Hint, hidden),
	}
	if rec.Args == nil {
		rec.Args = []string{}
	}
	return rec
}

// hiddenValues are the argument values the redactor replaced (whole
// arguments, or the value after "="), longest first, so the same text is
// removed from error messages. Values under three bytes are skipped: they
// would mangle ordinary words.
func hiddenValues(args, logged []string) []string {
	var out []string
	for i, a := range args {
		if i >= len(logged) || a == logged[i] {
			continue
		}
		v := a
		if strings.HasPrefix(a, "-") {
			_, val, ok := strings.Cut(a, "=")
			if !ok {
				continue
			}
			v = val
		}
		if len(v) >= 3 {
			out = append(out, v)
		}
	}
	sortLongestFirst(out)
	return out
}

func sortLongestFirst(v []string) {
	sort.SliceStable(v, func(i, j int) bool { return len(v[i]) > len(v[j]) })
}

// logHint redacts a suggested command the way arguments are: it is built
// from what the user typed (`rex rm <sel> -force`).
func logHint(hint string, hidden []string) string {
	f := strings.Fields(hint)
	if len(f) >= 2 && f[0] == "rex" {
		if _, known := FindCommand(f[1]); known {
			return logMessage(strings.Join(redact.Argv(f, cliPolicy(f[1], f[2:])), " "), hidden...)
		}
	}
	return logMessage(hint, hidden...)
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
