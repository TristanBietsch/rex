package core

import (
	"errors"
	"flag"
	"os"
	"time"

	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/wire/client"
)

// socketUsage is the -socket help text. The default is described rather
// than printed: it is resolved when the command runs, not when the flag is
// registered.
const socketUsage = "daemon socket `path` (default $XDG_RUNTIME_DIR/rex.sock, else ~/.cache/rex/rex.sock)"

// SocketFlag is the -socket flag of every command that talks to the daemon.
// Left unset, Path resolves the default socket (paths.Socket) when the
// command runs, so a missing home directory fails only the commands that
// need the socket, with an error that names -socket, instead of dialing a
// path relative to the working directory. A SocketFlag is a flag.Value; it
// is not safe for concurrent Set.
type SocketFlag struct{ path string }

// AddSocketFlag registers -socket on fs and returns its value.
func AddSocketFlag(fs *flag.FlagSet) *SocketFlag {
	s := &SocketFlag{}
	fs.Var(s, "socket", socketUsage)
	return s
}

// String implements flag.Value: the path given on the command line, or "".
func (s *SocketFlag) String() string {
	if s == nil {
		return ""
	}
	return s.path
}

// Set implements flag.Value.
func (s *SocketFlag) Set(v string) error {
	s.path = v
	return nil
}

// Path returns the socket given with -socket, else DefaultSocket.
func (s *SocketFlag) Path() (string, error) {
	if s != nil && s.path != "" {
		return s.path, nil
	}
	return DefaultSocket()
}

// Dial connects to the daemon on Path. A socket that cannot be resolved is
// an ExitGeneric error; a failed dial is ExitDaemonUnreachable.
func (s *SocketFlag) Dial() (*client.Client, error) {
	socket, err := s.Path()
	if err != nil {
		return nil, err
	}
	return DialPath(socket)
}

// DialPath connects to the daemon on socket. A failed dial is
// ExitDaemonUnreachable; when nothing listens there (no socket file, or one
// a dead daemon left behind) the message says so and how to start it.
func DialPath(socket string) (*client.Client, error) {
	c, err := client.Dial(socket)
	if err != nil {
		return nil, DaemonUnreachable(socket, err)
	}
	return c, nil
}

// DaemonUnreachable is the exit-4 error for a dial that failed with err.
func DaemonUnreachable(socket string, err error) error {
	if errors.Is(err, os.ErrNotExist) || connRefused(err) {
		ee := Errorf(ExitDaemonUnreachable, "rex daemon start",
			"rex-daemon is not running (no socket at %s); start it with `rex daemon start`", socket)
		ee.Err = err
		return ee.WithID("daemon_unreachable")
	}
	return NewExitError(ExitDaemonUnreachable, err.Error()).WithID("daemon_unreachable")
}

// HelloFailed is the exit-4 error for a handshake that failed with err: the
// daemon accepted the connection but did not answer.
func HelloFailed(err error) error {
	ee := Errorf(ExitDaemonUnreachable, "rex daemon status", "daemon did not answer the handshake: %s", err)
	ee.Err = err
	return ee.WithID("handshake_failed")
}

// DefaultSocket returns the daemon's default socket (paths.Socket). When it
// cannot be resolved (no home directory and no XDG_RUNTIME_DIR) the error is
// an ExitGeneric ExitError telling the user to pass -socket.
func DefaultSocket() (string, error) {
	socket, err := paths.Socket()
	if err != nil {
		return "", NewExitError(ExitGeneric, err.Error()+"; pass -socket")
	}
	return socket, nil
}

// DeadlineSeconds returns the time n seconds from now.
func DeadlineSeconds(n int) time.Time {
	return time.Now().Add(time.Duration(n) * time.Second)
}
