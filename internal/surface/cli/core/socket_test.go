package core

import (
	"errors"
	"flag"
	"io"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func TestSocketFlag(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_RUNTIME_DIR", "")

	parse := func(args ...string) *SocketFlag {
		fs := flag.NewFlagSet("t", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		s := AddSocketFlag(fs)
		require.NoError(t, fs.Parse(args))
		return s
	}

	got, err := parse().Path()
	require.NoError(t, err)
	require.Equal(t, filepath.Join(home, ".cache", "rex", "rex.sock"), got, "unset: the default socket")

	got, err = parse("-socket", "/x/rex.sock").Path()
	require.NoError(t, err)
	require.Equal(t, "/x/rex.sock", got)
	require.Equal(t, "/x/rex.sock", parse("-socket", "/x/rex.sock").String())
	require.Empty(t, (*SocketFlag)(nil).String())

	// A dial failure is ExitDaemonUnreachable.
	_, err = parse("-socket", filepath.Join(t.TempDir(), "none.sock")).Dial()
	var ee ExitError
	require.True(t, errors.As(err, &ee))
	require.Equal(t, ExitDaemonUnreachable, ee.Code)
}

// Without a home directory the default socket cannot be resolved: commands
// fail with ExitGeneric and say to pass -socket, instead of dialing a path
// relative to the working directory. An explicit -socket still works.
func TestSocketFlag_NoHome(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	t.Setenv("XDG_RUNTIME_DIR", "")

	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	s := AddSocketFlag(fs)
	require.NoError(t, fs.Parse(nil))
	for _, err := range []error{
		func() error { _, err := s.Path(); return err }(),
		func() error { _, err := s.Dial(); return err }(),
		func() error { _, err := DefaultSocket(); return err }(),
	} {
		var ee ExitError
		require.True(t, errors.As(err, &ee), "%v", err)
		require.Equal(t, ExitGeneric, ee.Code)
		require.Contains(t, ee.Msg, "pass -socket")
	}

	require.NoError(t, s.Set("/x/rex.sock"))
	got, err := s.Path()
	require.NoError(t, err)
	require.Equal(t, "/x/rex.sock", got)
}

// A daemon that refuses the handshake because the caller runs inside a
// session is up: the refusal is exit 6 with the daemon's reason, not the
// exit-4 "did not answer" that sends the user to check the daemon.
func TestHelloFailed_InSessionIsRefusedNotUnreachable(t *testing.T) {
	err := HelloFailed(&client.Error{Intent: protocol.IntentHello, Code: protocol.ErrCodeInSession,
		Message: "refused: this process runs inside a rex session"})
	var ee ExitError
	require.True(t, errors.As(err, &ee))
	require.Equal(t, ExitOperationRefused, ee.Code)
	require.Equal(t, "in_session", ee.ID)
	require.Contains(t, ee.Error(), "inside a rex session")
	require.NotContains(t, ee.Error(), "handshake")
}

func TestHelloFailed_OtherErrorsStayUnreachable(t *testing.T) {
	var ee ExitError
	require.True(t, errors.As(HelloFailed(errors.New("i/o timeout")), &ee))
	require.Equal(t, ExitDaemonUnreachable, ee.Code)
	require.Contains(t, ee.Error(), "did not answer the handshake")
}
