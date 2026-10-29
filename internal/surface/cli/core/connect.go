package core

import (
	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// Dial connects to the daemon at socket and sends Hello, returning the
// client and the board snapshot. Failures are ExitDaemonUnreachable. The
// caller closes the client.
func Dial(socket string) (*client.Client, *protocol.Snapshot, error) {
	c, err := client.Dial(socket)
	if err != nil {
		return nil, nil, NewExitError(ExitDaemonUnreachable, err.Error())
	}
	snap, err := c.Hello("rex-cli")
	if err != nil {
		_ = c.Close()
		return nil, nil, NewExitError(ExitDaemonUnreachable, err.Error())
	}
	return c, snap, nil
}

// Connect is Dial plus resolving sel (id, slug, short id or @state alias)
// in the snapshot. Selector errors keep their own exit codes. The caller
// closes the client.
func Connect(socket, sel string) (*client.Client, protocol.SessionSummary, error) {
	c, snap, err := Dial(socket)
	if err != nil {
		return nil, protocol.SessionSummary{}, err
	}
	sess, err := ResolveInSnapshot(snap.Sessions, sel)
	if err != nil {
		_ = c.Close()
		return nil, protocol.SessionSummary{}, err
	}
	return c, sess, nil
}
