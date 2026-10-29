// Package core holds what the rex CLI subcommands share: exit codes,
// argument parsing, selectors, output formatting, and help.
package core

import (
	"fmt"
	"runtime/debug"
)

// Version is the rex release label. Release builds set it with
//
//	-ldflags "-X github.com/tristanbietsch/rex/internal/surface/cli/core.Version=v1.2.3"
//
// Otherwise it is the module version from the build info (set by
// `go install …@v1.2.3`), or "dev".
var Version = "dev"

func init() { Version = resolveVersion(Version, debug.ReadBuildInfo) }

// resolveVersion keeps an ldflags-set v, else falls back to the main
// module's version from readBuildInfo, else "dev".
func resolveVersion(v string, readBuildInfo func() (*debug.BuildInfo, bool)) string {
	if v != "" && v != "dev" {
		return v
	}
	if bi, ok := readBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return "dev"
}

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
