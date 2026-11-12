package setup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/runtime/selfupdate"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

// fakeRelease is a releaseSource that knows the latest tag and fails Fetch
// (tests that reach Fetch use a real selfupdate.Source).
type fakeRelease struct {
	tag       string
	latestErr error
	fetches   int
}

func (r *fakeRelease) Latest(context.Context) (string, error) { return r.tag, r.latestErr }

func (r *fakeRelease) Fetch(context.Context, string, string, string, string) (*selfupdate.Archive, error) {
	r.fetches++
	return nil, errors.New("fake release cannot download")
}

// releaseServer serves a GitHub-shaped release for linux/amd64 over TLS:
// the latest tag from the API and each tag's archive and SHA256SUMS.
func releaseServer(t *testing.T, latest string, tags ...string) *selfupdate.Source {
	t.Helper()
	files := map[string][]byte{}
	for _, tag := range tags {
		ver := strings.TrimPrefix(tag, "v")
		dir := "rex_" + ver + "_linux_amd64"
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		tw := tar.NewWriter(gz)
		for _, e := range []struct{ name, body string }{
			{dir + "/rex", "rex " + tag},
			{dir + "/rex-daemon", "rex-daemon " + tag},
			{dir + "/LICENSE", "MIT"},
		} {
			require.NoError(t, tw.WriteHeader(&tar.Header{Name: e.name, Typeflag: tar.TypeReg, Mode: 0o755, Size: int64(len(e.body))}))
			_, err := tw.Write([]byte(e.body))
			require.NoError(t, err)
		}
		require.NoError(t, tw.Close())
		require.NoError(t, gz.Close())
		sum := sha256.Sum256(buf.Bytes())
		files[tag+"/"+dir+".tar.gz"] = buf.Bytes()
		files[tag+"/SHA256SUMS"] = []byte(hex.EncodeToString(sum[:]) + "  " + dir + ".tar.gz\n")
	}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/api/repos/o/rex/releases/latest" {
			_, _ = fmt.Fprintf(w, `{"tag_name":%q}`, latest)
			return
		}
		body, ok := files[strings.TrimPrefix(req.URL.Path, "/dl/o/rex/releases/download/")]
		if !ok {
			http.NotFound(w, req)
			return
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	return &selfupdate.Source{HTTP: srv.Client(), API: srv.URL + "/api", Download: srv.URL + "/dl", Repo: "o/rex", Hosts: []string{u.Host}}
}

// releaseFixture is an update fixture for a release install in a temp bin
// folder holding the old binaries.
func releaseFixture(t *testing.T, latest string, tags ...string) (*updateFixture, string) {
	t.Helper()
	f := newUpdateFixture()
	bin := t.TempDir()
	f.mode = installModeRelease
	f.exe = filepath.Join(bin, "rex")
	f.tempDir = t.TempDir()
	f.release = releaseServer(t, latest, tags...)
	for _, b := range []string{"rex", "rex-daemon"} {
		require.NoError(t, os.WriteFile(filepath.Join(bin, b), []byte(b+" old"), 0o755))
	}
	return f, bin
}

func requireBinaries(t *testing.T, bin, want string) {
	t.Helper()
	for _, b := range []string{"rex", "rex-daemon"} {
		got, err := os.ReadFile(filepath.Join(bin, b))
		require.NoError(t, err)
		require.Equal(t, b+" "+want, string(got), b)
	}
	ents, err := os.ReadDir(bin)
	require.NoError(t, err)
	require.Len(t, ents, 2, "no temp files left behind")
}

// A release install updates from GitHub releases and leaves the daemon
// running; it then says how to move the daemon over.
func TestRunUpdate_releaseInstallsAndLeavesDaemonRunning(t *testing.T) {
	f, bin := releaseFixture(t, "v2.1.0", "v2.1.0")

	require.NoError(t, f.run("-yes"))
	requireBinaries(t, bin, "v2.1.0")
	require.Equal(t, []string{"detect", "probe upgrade"}, f.log)
	require.Zero(t, f.daemon.stopCount())
	require.True(t, f.daemon.up)
	require.Contains(t, f.out.String(), "rex v2.0.0 → v2.1.0")
	require.Contains(t, f.out.String(), "rex daemon restart")
	require.Contains(t, f.out.String(), "ends running sessions")
}

func TestRunUpdate_releaseSuggestsUpgradeWhenAvailable(t *testing.T) {
	f, bin := releaseFixture(t, "v2.1.0", "v2.1.0")
	f.hasUpgrade = true

	require.NoError(t, f.run("-yes"))
	requireBinaries(t, bin, "v2.1.0")
	require.Contains(t, f.out.String(), "rex upgrade")
	require.NotContains(t, f.out.String(), "rex daemon restart")
}

func TestRunUpdate_releaseNoDaemonNoNextStep(t *testing.T) {
	f, bin := releaseFixture(t, "v2.1.0", "v2.1.0")
	f.daemon.up = false

	require.NoError(t, f.run("-yes", "-o", "json"))
	requireBinaries(t, bin, "v2.1.0")
	require.Equal(t, []string{"detect"}, f.log)
	require.Contains(t, f.out.String(), `"to":"v2.1.0"`)
	require.NotContains(t, f.out.String(), `"next"`)
}

func TestRunUpdate_releaseDowngradeRefusedWithoutForce(t *testing.T) {
	f, bin := releaseFixture(t, "v2.1.0", "v2.1.0", "v1.9.0")

	err := f.run("-yes", "-version", "v1.9.0")
	requireExitCode(t, err, core.ExitOperationRefused)
	require.ErrorContains(t, err, "older than the running v2.0.0")
	requireBinaries(t, bin, "old")

	require.NoError(t, f.run("-yes", "-version", "1.9.0", "-force"))
	requireBinaries(t, bin, "v1.9.0")
}

func TestRunUpdate_releaseUnknownVersionNeedsForce(t *testing.T) {
	for _, v := range []string{"dev", "v0.0.0-20261112093000-abcdef123456"} {
		f, bin := releaseFixture(t, "v2.1.0", "v2.1.0")
		f.version = v

		err := f.run("-yes")
		requireExitCode(t, err, core.ExitOperationRefused)
		require.ErrorContains(t, err, "-force")
		requireBinaries(t, bin, "old")

		require.NoError(t, f.run("-yes", "-force"))
		requireBinaries(t, bin, "v2.1.0")
	}
}

func TestRunUpdate_releaseAlreadyCurrent(t *testing.T) {
	f, bin := releaseFixture(t, "v2.0.0", "v2.0.0")

	require.NoError(t, f.run("-yes"))
	require.Contains(t, f.out.String(), "up to date")
	requireBinaries(t, bin, "old")
}

func TestRunUpdate_releasePinnedByEnv(t *testing.T) {
	f, bin := releaseFixture(t, "v2.2.0", "v2.1.0", "v2.2.0")
	f.env = map[string]string{"REX_VERSION": "v2.1.0"}

	require.NoError(t, f.run("-yes"))
	requireBinaries(t, bin, "v2.1.0")
}

func TestRunUpdate_releaseBadVersionFlag(t *testing.T) {
	f, bin := releaseFixture(t, "v2.1.0", "v2.1.0")
	requireExitCode(t, f.run("-yes", "-version", "v2.1.0/../../x"), core.ExitInvalidArgs)
	requireBinaries(t, bin, "old")
}

func TestRunUpdate_releaseMissingReleaseFails(t *testing.T) {
	f, bin := releaseFixture(t, "v2.1.0", "v2.1.0")
	err := f.run("-yes", "-version", "v3.0.0")
	requireExitCode(t, err, core.ExitGeneric)
	require.ErrorContains(t, err, "404")
	requireBinaries(t, bin, "old")
}

func TestRunUpdate_releaseNeedsYesWithoutTerminal(t *testing.T) {
	f, bin := releaseFixture(t, "v2.1.0", "v2.1.0")
	err := f.run()
	requireExitCode(t, err, core.ExitOperationRefused)
	require.ErrorContains(t, err, "-yes")
	requireBinaries(t, bin, "old")
}

func TestRunUpdate_releaseDeclinedChangesNothing(t *testing.T) {
	f, bin := releaseFixture(t, "v2.1.0", "v2.1.0")
	f.stdin = "\n" // default is no
	requireExitCode(t, f.run(), core.ExitOperationRefused)
	requireBinaries(t, bin, "old")
	require.Contains(t, f.errOut.String(), "v2.1.0")

	f.stdin = "y\n"
	require.NoError(t, f.run())
	requireBinaries(t, bin, "v2.1.0")
}

func TestRunUpdate_releaseLookupFailure(t *testing.T) {
	f := newUpdateFixture()
	f.mode = installModeRelease
	f.exe = filepath.Join(t.TempDir(), "rex")
	f.release = &fakeRelease{latestErr: errors.New("rate limited")}
	err := f.run("-yes")
	requireExitCode(t, err, core.ExitGeneric)
	require.ErrorContains(t, err, "rate limited")
	require.Zero(t, f.daemon.stopCount())
}

// A renamed binary (rex2) is not overwritten by a file called rex.
func TestRunUpdate_releaseRefusesRenamedBinary(t *testing.T) {
	f := newUpdateFixture()
	f.mode = installModeRelease
	f.exe = filepath.Join(t.TempDir(), "rex2")
	err := f.run("-yes")
	requireExitCode(t, err, core.ExitOperationRefused)
	require.Zero(t, f.release.(*fakeRelease).fetches)
}

func TestRunUpdate_packageManagerRefused(t *testing.T) {
	f := newUpdateFixture()
	f.mode = installModePackage
	f.exe = filepath.Join(string(filepath.Separator)+"opt", "homebrew", "Cellar", "rex", "2.0.0", "bin", "rex")
	err := f.run("-yes")
	requireExitCode(t, err, core.ExitOperationRefused)
	require.ErrorContains(t, err, "Homebrew")
	require.Equal(t, []string{"detect"}, f.log)
	require.Zero(t, f.daemon.stopCount())
	require.Zero(t, f.release.(*fakeRelease).fetches)
}

func TestRunUpdate_sourceCheckoutRefused(t *testing.T) {
	repo := t.TempDir()
	for _, n := range []string{"install.sh", "go.mod"} {
		require.NoError(t, os.WriteFile(filepath.Join(repo, n), nil, 0o600))
	}
	f := newUpdateFixture()
	f.mode = installModeRelease
	f.exe = filepath.Join(repo, "rex")
	err := f.run("-yes")
	requireExitCode(t, err, core.ExitOperationRefused)
	require.ErrorContains(t, err, "source checkout")
	require.Zero(t, f.daemon.stopCount())
}

func TestRunUpdate_checkStatuses(t *testing.T) {
	for _, c := range []struct{ running, latest, want string }{
		{"v2.0.0", "v2.1.0", statusAvailable},
		{"v2.1.0", "v2.1.0", statusCurrent},
		{"v2.2.0", "v2.1.0", statusNewer},
		{"dev", "v2.1.0", statusUnknown},
		{"v0.0.0-20261112093000-abcdef123456", "v2.1.0", statusUnknown},
	} {
		f := newUpdateFixture()
		f.version = c.running
		f.release = &fakeRelease{tag: c.latest}
		require.NoError(t, f.run("-check", "-o", "json"))
		require.Contains(t, f.out.String(), `"status":"`+c.want+`"`, c)
		require.Contains(t, f.out.String(), `"remote":"`+c.latest+`"`, c)
		require.Empty(t, f.log, "-check touches nothing")
		require.Zero(t, f.release.(*fakeRelease).fetches)
	}
}

func TestRunUpdate_checkPinnedAndFailure(t *testing.T) {
	f := newUpdateFixture()
	f.release = &fakeRelease{latestErr: errors.New("offline")}
	require.NoError(t, f.run("-check", "-version", "v1.0.0", "-o", "json"))
	require.Contains(t, f.out.String(), `"status":"running-newer"`)

	err := f.run("-check")
	requireExitCode(t, err, core.ExitGeneric)
	require.ErrorContains(t, err, "offline")
}

func TestPackageManagerOf(t *testing.T) {
	require.Equal(t, "Homebrew", packageManagerOf("/usr/local/Cellar/rex/2.0.0/bin/rex"))
	require.Equal(t, "Scoop", packageManagerOf(`C:\Users\a\scoop\apps\rex\current\rex.exe`))
	require.Equal(t, "the system package manager", packageManagerOf("/usr/bin/rex"))
	require.Empty(t, packageManagerOf("/home/a/.local/bin/rex"))
	require.Empty(t, packageManagerOf(`C:\Users\a\.local\bin\rex.exe`))
}
