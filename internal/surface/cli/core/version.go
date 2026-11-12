// Package core holds what every rex CLI subcommand shares: exit codes,
// output helpers, session selectors and the -socket flag.
package core

import (
	"flag"

	"github.com/tristanbietsch/rex/internal/version"
)

// Version is the rex CLI release label (see package version). It is a
// variable because release builds set version.Version with -ldflags.
var Version = version.Version

// ExitCoder is any error that carries a specific exit code.
type ExitCoder interface {
	error
	ExitCode() int
}

// ExitError carries a numeric exit code alongside an error message.
type ExitError struct {
	Code int
	Msg  string
	// Hint is the command that fixes the problem ("rex daemon start"). The
	// human and structured formats show it; it is not part of Msg.
	Hint string
	// Err is the underlying cause, kept for errors.Is and errors.As. It is
	// not part of Msg.
	Err error
	// ID is a stable name for a handful of known errors (see errCodes). It is
	// the only part of an error's identity the error log keeps besides its
	// type; it is never formatted text.
	ID string
}

// WithID returns e with its stable ID set.
func (e ExitError) WithID(id string) ExitError {
	e.ID = id
	return e
}

// Error implements error.
func (e ExitError) Error() string { return e.Msg }

// Unwrap returns the underlying cause, if any.
func (e ExitError) Unwrap() error { return e.Err }

// ExitCode implements ExitCoder.
func (e ExitError) ExitCode() int { return e.Code }

// NewExitError wraps a message with an exit code.
func NewExitError(code int, msg string) ExitError { return ExitError{Code: code, Msg: msg} }

// Exit code constants (mirrors docs/agents/interfaces/cli.md exit-codes section).
const (
	ExitOK                = 0
	ExitGeneric           = 1
	ExitSelectorNotFound  = 2
	ExitAmbiguousSelector = 3
	ExitDaemonUnreachable = 4
	ExitInvalidArgs       = 5
	ExitOperationRefused  = 6
	ExitWaitTimedOut      = 7
)

// versionData is the `-o json` payload of rex version.
type versionData struct {
	Version string `json:"version"`
}

// RunVersion prints the binary version.
func RunVersion(args []string) error {
	fs := flag.NewFlagSet("version", flag.ContinueOnError)
	out := AddOutputFlags(fs, AllFormats)
	if _, err := out.Parse(fs, args); err != nil {
		return err
	}
	return Out().Emit(Result{
		Cmd:  "version",
		Data: versionData{Version: Version},
		View: ActionView(Line{Text: Version, Txt: Version}, Field{Key: "version", Val: Version}),
	})
}
