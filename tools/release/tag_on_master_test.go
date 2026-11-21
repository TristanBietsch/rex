//go:build !windows

// Package release_test runs tools/release/tag_on_master.sh against a throwaway
// git repository, so the real checkout and its remotes are never touched.
package release_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// git runs git in dir with a fixed identity, no global config and no signing.
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false"}, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	return strings.TrimSpace(string(out))
}

// runScript runs the script in dir and returns its exit code and stderr.
func runScript(t *testing.T, dir string, args ...string) (int, string) {
	t.Helper()
	script, err := os.Getwd()
	require.NoError(t, err)
	cmd := exec.Command("sh", append([]string{script + "/tag_on_master.sh"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err == nil {
		return 0, stderr.String()
	}
	var ee *exec.ExitError
	require.ErrorAs(t, err, &ee)
	return ee.ExitCode(), stderr.String()
}

// newRepo makes a repo whose refs/remotes/origin/master holds one commit
// (onMaster) and whose HEAD is a second commit on another branch (offMaster).
func newRepo(t *testing.T) (dir, onMaster, offMaster string) {
	t.Helper()
	dir = t.TempDir()
	git(t, dir, "init", "-q", "-b", "master")
	git(t, dir, "commit", "-q", "--allow-empty", "-m", "on master")
	onMaster = git(t, dir, "rev-parse", "HEAD")
	git(t, dir, "update-ref", "refs/remotes/origin/master", onMaster)
	git(t, dir, "checkout", "-q", "-b", "side")
	git(t, dir, "commit", "-q", "--allow-empty", "-m", "off master")
	offMaster = git(t, dir, "rev-parse", "HEAD")
	return dir, onMaster, offMaster
}

func TestTagOnMaster_Accepts(t *testing.T) {
	dir, on, _ := newRepo(t)
	code, stderr := runScript(t, dir, on, "v1.2.3")
	require.Equal(t, 0, code, stderr)
}

func TestTagOnMaster_RefusesOffMaster(t *testing.T) {
	dir, _, off := newRepo(t)
	code, stderr := runScript(t, dir, off, "v1.2.3")
	require.Equal(t, 1, code)
	require.Contains(t, stderr, "v1.2.3")
	require.Contains(t, stderr, off)
	require.Contains(t, stderr, "master")
}

func TestTagOnMaster_NoOriginMasterFails(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "master")
	git(t, dir, "commit", "-q", "--allow-empty", "-m", "x")
	code, stderr := runScript(t, dir, git(t, dir, "rev-parse", "HEAD"), "v1.0.0")
	require.Equal(t, 1, code)
	require.Contains(t, stderr, "origin/master")
}

func TestTagOnMaster_UsageError(t *testing.T) {
	dir, _, _ := newRepo(t)
	code, stderr := runScript(t, dir)
	require.Equal(t, 2, code)
	require.Contains(t, stderr, "usage")
}

func TestTagOnMaster_AnnotatedTagPeeled(t *testing.T) {
	dir, on, off := newRepo(t)
	git(t, dir, "tag", "-a", "-m", "m", "v-on", on)
	git(t, dir, "tag", "-a", "-m", "m", "v-off", off)
	onObj := git(t, dir, "rev-parse", "v-on")
	offObj := git(t, dir, "rev-parse", "v-off")
	require.NotEqual(t, on, onObj)

	code, stderr := runScript(t, dir, onObj, "v-on")
	require.Equal(t, 0, code, stderr)
	code, stderr = runScript(t, dir, offObj, "v-off")
	require.Equal(t, 1, code)
	require.Contains(t, stderr, off)
}

func TestTagOnMaster_RefusesOptionLikeArgument(t *testing.T) {
	dir, _, _ := newRepo(t)
	for _, arg := range []string{"--help", "-x"} {
		code, stderr := runScript(t, dir, arg, "v1.0.0")
		require.Equal(t, 1, code, arg)
		require.NotContains(t, stderr, "usage: git")
	}
}

func TestTagOnMaster_UnknownCommitFails(t *testing.T) {
	dir, _, _ := newRepo(t)
	code, stderr := runScript(t, dir, "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef", "v1.0.0")
	require.Equal(t, 1, code)
	require.Contains(t, stderr, "v1.0.0")
}
