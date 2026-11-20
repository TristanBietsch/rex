package server

import "fmt"

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
