package snapshot

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// Errors callers match with errors.Is.
var (
	// ErrNotGitWorktree means the folder is not inside a git worktree, so it
	// is never captured.
	ErrNotGitWorktree = errors.New("snapshots need a git repo: the folder is not inside a git worktree")
	// ErrNoGit means no git binary was found on PATH.
	ErrNoGit = errors.New("git not found on PATH")
	// ErrGitTooOld means git predates GIT_CONFIG_GLOBAL (2.32), which keeps
	// the user's global config out of every call.
	ErrGitTooOld = errors.New("git is older than 2.32: snapshots need it to ignore global config")
	// ErrDubiousOwnership means git refused the folder as owned by someone
	// else (safe.directory). It is not "not a git repo".
	ErrDubiousOwnership = errors.New("git treats the folder as unsafe (dubious ownership), so it cannot be snapshotted")
)

// safeConfig are the -c settings on every shadow git call. They keep the
// user's hooks, line-ending conversion, exclude files and background work
// out of a capture.
func safeConfig() []string {
	return []string{
		"-c", "core.hooksPath=" + os.DevNull,
		"-c", "core.autocrlf=false",
		"-c", "core.safecrlf=false",
		"-c", "core.excludesFile=" + os.DevNull,
		"-c", "core.fsmonitor=false",
		"-c", "core.quotepath=off",
		"-c", "gc.auto=0",
		"-c", "maintenance.auto=false",
		"-c", "advice.addEmbeddedRepo=false",
	}
}

// baseEnv is the process environment without any GIT_* variable, so a
// rex-daemon started from a git hook cannot be steered by its parent.
func baseEnv() []string {
	var out []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(strings.ToUpper(kv), "GIT_") {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// gitEnv is the one builder of the environment for shadow git calls: the
// shadow dir as GIT_DIR, the user's folder as GIT_WORK_TREE and the given
// index file. No system or global config is read, nothing prompts.
func gitEnv(shadow, cwd, index string) []string {
	return append(baseEnv(),
		"GIT_DIR="+shadow,
		"GIT_WORK_TREE="+cwd,
		"GIT_INDEX_FILE="+index,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_TERMINAL_PROMPT=0",
		"GIT_OPTIONAL_LOCKS=0",
		"LC_ALL=C",
	)
}

// readOnlyEnv is the environment for looking into the user's own repo
// (rev-parse only): no config outside the repo, no optional locks, so
// nothing in the repo is written.
func readOnlyEnv() []string {
	// LC_ALL=C keeps git's messages untranslated: callers match on them.
	return append(baseEnv(),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_TERMINAL_PROMPT=0",
		"GIT_OPTIONAL_LOCKS=0",
		"LC_ALL=C",
	)
}

// run executes git with args in dir and returns stdout. stdin may be nil.
func run(ctx context.Context, env []string, dir string, stdin []byte, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = env
	cmd.Dir = dir
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		if ce := ctx.Err(); ce != nil {
			return nil, fmt.Errorf("git %s: %w", subcommand(args), ce)
		}
		return nil, fmt.Errorf("git %s: %w: %s", subcommand(args), err, strings.TrimSpace(errb.String()))
	}
	return out.Bytes(), nil
}

// subcommand names the git subcommand in a "-c k=v ..." argument list.
func subcommand(args []string) string {
	for i := 0; i < len(args); i++ {
		if args[i] == "-c" {
			i++
			continue
		}
		return args[i]
	}
	return ""
}

var (
	gitVerMu    sync.Mutex
	gitVerCache = map[string]error{}
)

var gitVersionRe = regexp.MustCompile(`(\d+)\.(\d+)`)

// CheckGit reports ErrNoGit when git is missing and ErrGitTooOld when it is
// older than 2.32. The answer is remembered per git path.
func CheckGit(ctx context.Context) error {
	path, err := GitPath()
	if err != nil {
		return err
	}
	gitVerMu.Lock()
	defer gitVerMu.Unlock()
	if r, ok := gitVerCache[path]; ok {
		return r
	}
	out, err := run(ctx, baseEnv(), "", nil, "--version")
	if err != nil {
		return err
	}
	m := gitVersionRe.FindStringSubmatch(string(out))
	if m == nil {
		return fmt.Errorf("snapshot: cannot read git version from %q", strings.TrimSpace(string(out)))
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	var res error
	if major < 2 || (major == 2 && minor < 32) {
		res = fmt.Errorf("%w (found %d.%d)", ErrGitTooOld, major, minor)
	}
	gitVerCache[path] = res
	return res
}

// GitPath returns the git binary's path, or ErrNoGit.
func GitPath() (string, error) {
	p, err := exec.LookPath("git")
	if err != nil {
		return "", ErrNoGit
	}
	return p, nil
}
