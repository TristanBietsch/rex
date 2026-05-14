// Package cli implements the rex CLI subcommands.
package cli

import "fmt"

const version = "0.0.2-plan-b"

// ExitCoder is any error that carries a specific exit code.
type ExitCoder interface {
	error
	ExitCode() int
}

// ExitError carries a numeric exit code alongside an error message.
type ExitError struct {
	Code int
	Msg  string
}

// Error implements error.
func (e ExitError) Error() string { return e.Msg }

// ExitCode implements ExitCoder.
func (e ExitError) ExitCode() int { return e.Code }

// NewExitError wraps a message with an exit code.
func NewExitError(code int, msg string) ExitError { return ExitError{Code: code, Msg: msg} }

// Exit code constants (mirrors docs/cli.md exit-codes section).
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

// RunVersion prints the binary version.
func RunVersion() error {
	fmt.Println(version)
	return nil
}

// notImplemented is the placeholder body for stubs that will be replaced in later tasks.
func notImplemented(verb string) error {
	return NewExitError(ExitGeneric, fmt.Sprintf("%s: not implemented yet", verb))
}

// Verb stubs — each gets replaced by its own file in subsequent tasks (B6-B17).
// Keep these exported so cmd/rex/main.go compiles between tasks.

func RunWait(args []string) error       { _ = args; return notImplemented("wait") }
func RunReload(args []string) error     { _ = args; return notImplemented("reload") }
func RunDaemon(args []string) error     { _ = args; return notImplemented("daemon") }
func RunCompletion(args []string) error { _ = args; return notImplemented("completion") }
