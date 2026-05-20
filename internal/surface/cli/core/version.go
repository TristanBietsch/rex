// Package cli implements the rex CLI subcommands.
package core

import "fmt"

// Version is the rex CLI release label.
const Version = "v1"

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

// RunVersion prints the binary version.
func RunVersion() error {
	fmt.Println(Version)
	return nil
}
