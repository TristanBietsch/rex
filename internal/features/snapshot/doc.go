// Package snapshot records a working folder's files at agent turn
// boundaries into a shadow git store that rex owns, and restores them for
// undo and redo.
//
// One shadow git dir exists per folder (not per session):
//
//	<root>/<sha256(abs cwd)[:16]>/git          the shadow GIT_DIR, 0700
//	<root>/<sha256(abs cwd)[:16]>/turns.jsonl  the ledger, one line per capture
//
// Every git call runs the git binary with an explicit environment (gitEnv):
// the user's repo, index, config and hooks are never changed. A capture is
// "git add -A" into the session's private index plus "git write-tree"; a ref
// refs/rex/<session>/<n> keeps the tree alive. Only folders inside a git
// worktree are captured (ErrNotGitWorktree).
package snapshot
