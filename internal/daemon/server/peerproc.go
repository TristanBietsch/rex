package server

import (
	"errors"
	"fmt"
	"log/slog"
	"net"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// peerClass is what the daemon knows about the process behind a connection.
type peerClass struct {
	// pid is the peer's pid; 0 when the platform does not report it.
	pid int
	// inSession is set when the peer runs inside a session this daemon
	// supervises (an agent, or any process an agent started), or when that
	// cannot be ruled out. Such a peer may send only inSessionAllowed
	// intents.
	inSession bool
}

// classifyPeer reads the pid behind conn and whether it descends from this
// daemon. Every agent is the daemon's child, so an agent's tool calls (a
// shell running `rex send`, a script) descend from it; the board and CLI the
// user runs in a terminal do not. A connection the daemon makes to itself
// does not count: a process is not its own descendant.
//
// It fails closed: when the pid or the ancestry cannot be read (the peer
// exited, or sits in a pid namespace the daemon cannot see), the peer is
// treated as in-session. On platforms without peer pids (Windows) nothing
// is classified, and the socket's access control is the only guard.
//
// This stops agents from driving sessions through rex by accident or by a
// simple command. It is not a boundary against a process of the same user
// that sets out to evade it: one that detaches from its parents (double
// fork, setsid and exit) is re-parented outside the daemon's tree.
func (s *Server) classifyPeer(conn net.Conn) peerClass {
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		return peerClass{}
	}
	pid, err := peerPID(uc)
	if errors.Is(err, errPeerCredUnsupported) {
		return peerClass{}
	}
	if err != nil || pid <= 0 {
		slog.Warn("server: peer pid unknown; treating peer as in-session", "pid", pid, "err", err)
		return peerClass{pid: pid, inSession: true}
	}
	in, err := descendsFrom(pid, s.selfPID)
	if err != nil {
		slog.Warn("server: peer ancestry unknown; treating peer as in-session", "pid", pid, "err", err)
		return peerClass{pid: pid, inSession: true}
	}
	return peerClass{pid: pid, inSession: in}
}

// inSessionAllowed reports whether a peer inside a session may send intent
// type typ. Only the Claude status-line usage tap, which rex itself adds to
// each session, talks to the daemon from inside one.
func inSessionAllowed(typ string) bool {
	return typ == protocol.IntentUsageSample
}

// inSessionRefusal is the message of the in_session Error.
const inSessionRefusal = "refused: this process runs inside a rex session (an agent or a tool it started); " +
	"agents cannot control rex. Run rex from your own terminal"

// maxAncestry bounds the parent walk in descendsFrom. Real process trees are
// a few dozen levels deep; the bound only stops a corrupt or cyclic answer
// from looping forever.
const maxAncestry = 1024

// descendsFrom reports whether process pid is a descendant (a child, a
// grandchild, ...) of process ancestor. A process does not descend from
// itself. The walk ends at pid 1 (init/launchd) or below.
//
// It reads the live process table, so it answers for the moment it runs: a
// process whose parent exited has been re-parented (to pid 1 or a subreaper)
// and no longer descends from its original ancestors. An error means the
// ancestry could not be read (pid or one of its ancestors exited mid-walk,
// or the platform has no process table support); callers that gate on the
// answer must treat an error as "cannot tell".
func descendsFrom(pid, ancestor int) (bool, error) {
	for range maxAncestry {
		if pid <= 1 {
			return false, nil
		}
		ppid, err := parentPID(pid)
		if err != nil {
			return false, err
		}
		if ppid == ancestor {
			return true, nil
		}
		if ppid == pid {
			return false, fmt.Errorf("process %d: reports itself as its parent", pid)
		}
		pid = ppid
	}
	return false, fmt.Errorf("process %d: ancestry deeper than %d levels", pid, maxAncestry)
}
