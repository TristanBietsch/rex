package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"syscall"

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
	// unknown is set when inSession is set only because the pid, the
	// ancestry or the session could not be read.
	unknown bool
}

// classifyPeer reads the pid behind conn and decides whether the peer runs
// inside a session this daemon supervises. Every agent is the daemon's child
// and leads its own session (its PTY), so a process an agent starts either
// descends from the daemon or, when it left the daemon's tree (`(cmd &)`,
// nohup, a double fork), still runs in the agent's session. The board and
// CLI the user runs in a terminal do neither. A connection the daemon makes
// to itself does not count.
//
// It fails closed: when the pid, the ancestry or the session cannot be read
// (the peer exited, or sits in a pid namespace the daemon cannot see), the
// peer is treated as in-session. On platforms without peer pids (Windows)
// nothing is classified, and the socket's access control is the only guard.
//
// This stops agents from driving sessions through rex by accident or with
// ordinary shell. It is not a boundary against a process of the same user
// that sets out to evade it: one that starts a new session (setsid) and
// then leaves the daemon's tree is treated as the user's.
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
		logUnknownPeer("pid", pid, err)
		return peerClass{pid: pid, inSession: true, unknown: true}
	}
	if pid == s.selfPID {
		return peerClass{pid: pid}
	}
	in, err := descendsFrom(pid, s.selfPID)
	if err == nil && !in {
		in, err = inDaemonSession(pid, s.selfPID)
	}
	if err != nil {
		logUnknownPeer("ancestry", pid, err)
		return peerClass{pid: pid, inSession: true, unknown: true}
	}
	return peerClass{pid: pid, inSession: in}
}

// logUnknownPeer logs a peer that classifyPeer could not classify. A peer
// that hung up before accept (a liveness probe: connect, then close) is
// routine, so that is Debug; anything else is a Warn.
func logUnknownPeer(what string, pid int, err error) {
	level := slog.LevelWarn
	if errors.Is(err, syscall.ENOTCONN) {
		level = slog.LevelDebug
	}
	slog.Log(context.Background(), level, "server: peer "+what+" unknown; treating peer as in-session", "pid", pid, "err", err)
}

// inDaemonSession reports whether process pid runs in a session led by the
// daemon or by one of its descendants (an agent: each leads the session of
// its PTY). A process keeps its session when it leaves the daemon's tree;
// only setsid starts a new one. The daemon's own process is not inside a
// session. An error means the session or its leader could not be read.
func inDaemonSession(pid, daemon int) (bool, error) {
	if pid == daemon {
		return false, nil
	}
	sid, err := sessionOf(pid)
	if err != nil {
		return false, err
	}
	switch {
	case sid == daemon:
		return true, nil
	case sid <= 1 || sid == pid:
		// No session, or pid leads its own: its ancestry was the answer.
		return false, nil
	}
	return descendsFrom(sid, daemon)
}

// inSessionAllowed reports whether a peer inside a session may send intent
// type typ. Only the Claude status-line usage tap, which rex itself adds to
// each session, talks to the daemon from inside one.
func inSessionAllowed(typ string) bool {
	return typ == protocol.IntentUsageSample
}

// Messages of the in_session Error: for a peer found inside a session, and
// for one the daemon could not classify (fail closed).
const (
	inSessionRefusal = "refused: this process runs inside a rex session (an agent or a tool it started); " +
		"agents cannot control rex. Run rex from your own terminal"
	inSessionUnknownRefusal = "refused: rex could not tell whether this process runs inside a rex session " +
		"(its pid, parents or session could not be read); run rex from your own terminal"
)

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
