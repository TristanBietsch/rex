package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// entry is one file in a test archive.
type entry struct {
	name string
	body string
	typ  byte // tar type; 0 is a regular file
	link string
	size int64 // header size override for tar (0: len(body))
}

func tarGz(t *testing.T, entries []entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		typ := e.typ
		if typ == 0 {
			typ = tar.TypeReg
		}
		h := &tar.Header{Name: e.name, Typeflag: typ, Mode: 0o755, Linkname: e.link}
		if typ == tar.TypeReg {
			h.Size = int64(len(e.body))
		}
		require.NoError(t, tw.WriteHeader(h))
		if typ == tar.TypeReg {
			_, err := tw.Write([]byte(e.body))
			require.NoError(t, err)
		}
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	return buf.Bytes()
}

func zipOf(t *testing.T, entries []entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		mode := os.FileMode(0o755)
		if e.typ == tar.TypeSymlink {
			mode = os.ModeSymlink | 0o777
		}
		h.SetMode(mode)
		w, err := zw.CreateHeader(h)
		require.NoError(t, err)
		_, err = w.Write([]byte(e.body))
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())
	return buf.Bytes()
}

func sumLine(name string, data []byte) string {
	s := sha256.Sum256(data)
	return hex.EncodeToString(s[:]) + "  " + name + "\n"
}

// release is a fake GitHub: the API and the download host on one TLS
// server.
type release struct {
	srv   *httptest.Server
	tag   string
	files map[string][]byte
	// redirect, when set, answers every download with a 302 to it.
	redirect string
	gotAuth  map[string]string
}

func newRelease(t *testing.T, tag string) *release {
	t.Helper()
	r := &release{tag: tag, files: map[string][]byte{}, gotAuth: map[string]string{}}
	r.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.gotAuth[req.URL.Path] = req.Header.Get("Authorization")
		switch {
		case req.URL.Path == "/api/repos/o/rex/releases/latest":
			_, _ = fmt.Fprintf(w, `{"tag_name":%q,"assets":[]}`, r.tag)
		case strings.HasPrefix(req.URL.Path, "/dl/o/rex/releases/download/"):
			if r.redirect != "" {
				http.Redirect(w, req, r.redirect+req.URL.Path, http.StatusFound)
				return
			}
			rest := strings.TrimPrefix(req.URL.Path, "/dl/o/rex/releases/download/")
			tag, name, _ := strings.Cut(rest, "/")
			body, ok := r.files[name]
			if tag != r.tag || !ok {
				http.NotFound(w, req)
				return
			}
			_, _ = w.Write(body)
		default:
			http.NotFound(w, req)
		}
	}))
	t.Cleanup(r.srv.Close)
	return r
}

func (r *release) host() string {
	u, _ := url.Parse(r.srv.URL)
	return u.Host
}

func (r *release) source() *Source {
	return &Source{
		HTTP:     r.srv.Client(),
		API:      r.srv.URL + "/api",
		Download: r.srv.URL + "/dl",
		Repo:     "o/rex",
		Hosts:    []string{r.host()},
	}
}

// publish adds a linux/amd64 tar.gz with the given entries and a matching
// SHA256SUMS.
func (r *release) publish(t *testing.T, entries []entry) string {
	t.Helper()
	ver := strings.TrimPrefix(r.tag, "v")
	name := "rex_" + ver + "_linux_amd64.tar.gz"
	r.files[name] = tarGz(t, entries)
	r.files["SHA256SUMS"] = []byte(sumLine(name, r.files[name]))
	return name
}

func goodEntries(dir string) []entry {
	return []entry{
		{name: dir + "/", typ: tar.TypeDir},
		{name: dir + "/rex", body: "new rex"},
		{name: dir + "/rex-daemon", body: "new daemon"},
		{name: dir + "/LICENSE", body: "MIT"},
		{name: dir + "/README.md", body: "readme"},
	}
}

func fetchAndStage(t *testing.T, s *Source, tag, goos string) (string, error) {
	t.Helper()
	dir := t.TempDir()
	a, err := s.Fetch(context.Background(), tag, goos, "amd64", t.TempDir())
	if err != nil {
		return dir, err
	}
	st, err := a.Stage(dir)
	if err != nil {
		return dir, err
	}
	return dir, st.Install()
}

func TestLatest(t *testing.T) {
	r := newRelease(t, "v2.1.0")
	s := r.source()
	s.Token = "tok"
	tag, err := s.Latest(context.Background())
	require.NoError(t, err)
	require.Equal(t, "v2.1.0", tag)
	require.Equal(t, "Bearer tok", r.gotAuth["/api/repos/o/rex/releases/latest"])
}

func TestLatest_rejectsOddTag(t *testing.T) {
	r := newRelease(t, "v2.1.0/../x")
	_, err := r.source().Latest(context.Background())
	require.ErrorContains(t, err, "unexpected tag")
}

func TestFetch_goodPath(t *testing.T) {
	r := newRelease(t, "v2.1.0")
	r.publish(t, goodEntries("rex_2.1.0_linux_amd64"))
	s := r.source()
	s.Token = "tok"
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "rex"), []byte("old rex"), 0o755))
	a, err := s.Fetch(context.Background(), "v2.1.0", "linux", "amd64", t.TempDir())
	require.NoError(t, err)
	require.Equal(t, "rex_2.1.0_linux_amd64.tar.gz", a.Name)
	st, err := a.Stage(dir)
	require.NoError(t, err)
	require.NoError(t, st.Install())

	for name, want := range map[string]string{"rex": "new rex", "rex-daemon": "new daemon"} {
		got, err := os.ReadFile(filepath.Join(dir, name))
		require.NoError(t, err)
		require.Equal(t, want, string(got))
		if runtime.GOOS != "windows" {
			fi, err := os.Stat(filepath.Join(dir, name))
			require.NoError(t, err)
			require.Equal(t, os.FileMode(0o755), fi.Mode().Perm())
		}
	}
	// Only the two binaries are written, and no temp files are left.
	ents, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, ents, 2)
	// The token goes to the API only, never to the download host.
	for path, auth := range r.gotAuth {
		if strings.HasPrefix(path, "/dl/") {
			require.Empty(t, auth, path)
		}
	}
}

func TestFetch_badChecksum(t *testing.T) {
	r := newRelease(t, "v2.1.0")
	name := r.publish(t, goodEntries("rex_2.1.0_linux_amd64"))
	r.files["SHA256SUMS"] = []byte(sumLine(name, []byte("something else")))
	dir, err := fetchAndStage(t, r.source(), "v2.1.0", "linux")
	require.ErrorContains(t, err, "checksum mismatch")
	requireEmptyDir(t, dir)
}

func TestFetch_missingPlatform(t *testing.T) {
	r := newRelease(t, "v2.1.0")
	r.publish(t, goodEntries("rex_2.1.0_linux_amd64"))
	_, err := fetchAndStage(t, r.source(), "v2.1.0", "darwin")
	require.ErrorContains(t, err, "no build for darwin/amd64")
}

func TestFetch_missingRelease(t *testing.T) {
	r := newRelease(t, "v2.1.0")
	r.publish(t, goodEntries("rex_2.1.0_linux_amd64"))
	_, err := fetchAndStage(t, r.source(), "v9.9.9", "linux")
	require.ErrorContains(t, err, "404")
}

func TestFetch_duplicateChecksumEntriesRefused(t *testing.T) {
	r := newRelease(t, "v2.1.0")
	name := r.publish(t, goodEntries("rex_2.1.0_linux_amd64"))
	r.files["SHA256SUMS"] = append(r.files["SHA256SUMS"], []byte(sumLine(name, []byte("x")))...)
	_, err := fetchAndStage(t, r.source(), "v2.1.0", "linux")
	require.ErrorContains(t, err, "more than once")
}

func TestFetch_pathTraversalRefused(t *testing.T) {
	for _, name := range []string{"../rex", "rex_2.1.0_linux_amd64/../../rex", "/rex", "rex_2.1.0_linux_amd64/sub/rex"} {
		t.Run(name, func(t *testing.T) {
			r := newRelease(t, "v2.1.0")
			r.publish(t, []entry{
				{name: name, body: "evil"},
				{name: "rex_2.1.0_linux_amd64/rex-daemon", body: "d"},
			})
			dir, err := fetchAndStage(t, r.source(), "v2.1.0", "linux")
			require.ErrorContains(t, err, "unexpected path")
			requireEmptyDir(t, dir)
		})
	}
}

func TestFetch_symlinkRefused(t *testing.T) {
	for _, typ := range []byte{tar.TypeSymlink, tar.TypeLink} {
		r := newRelease(t, "v2.1.0")
		r.publish(t, []entry{
			{name: "rex_2.1.0_linux_amd64/rex", typ: typ, link: "/etc/passwd"},
			{name: "rex_2.1.0_linux_amd64/rex-daemon", body: "d"},
		})
		dir, err := fetchAndStage(t, r.source(), "v2.1.0", "linux")
		require.ErrorContains(t, err, "not a regular file")
		requireEmptyDir(t, dir)
	}
}

func TestFetch_missingBinaryRefused(t *testing.T) {
	r := newRelease(t, "v2.1.0")
	r.publish(t, []entry{{name: "rex_2.1.0_linux_amd64/rex", body: "r"}})
	dir, err := fetchAndStage(t, r.source(), "v2.1.0", "linux")
	require.ErrorContains(t, err, "does not contain rex-daemon")
	requireEmptyDir(t, dir)
}

func TestFetch_duplicateEntryRefused(t *testing.T) {
	r := newRelease(t, "v2.1.0")
	r.publish(t, []entry{
		{name: "rex_2.1.0_linux_amd64/rex", body: "r"},
		{name: "rex_2.1.0_linux_amd64/rex", body: "r2"},
		{name: "rex_2.1.0_linux_amd64/rex-daemon", body: "d"},
	})
	dir, err := fetchAndStage(t, r.source(), "v2.1.0", "linux")
	require.ErrorContains(t, err, "twice")
	requireEmptyDir(t, dir)
}

func TestFetch_oversizedEntryRefused(t *testing.T) {
	r := newRelease(t, "v2.1.0")
	r.publish(t, []entry{
		{name: "rex_2.1.0_linux_amd64/rex", body: strings.Repeat("x", 2048)},
		{name: "rex_2.1.0_linux_amd64/rex-daemon", body: "d"},
	})
	s := r.source()
	s.MaxEntry = 1024
	dir, err := fetchAndStage(t, s, "v2.1.0", "linux")
	require.ErrorContains(t, err, "too large")
	requireEmptyDir(t, dir)
}

func TestFetch_oversizedArchiveRefused(t *testing.T) {
	r := newRelease(t, "v2.1.0")
	r.publish(t, goodEntries("rex_2.1.0_linux_amd64"))
	s := r.source()
	s.MaxArchive = 64
	_, err := fetchAndStage(t, s, "v2.1.0", "linux")
	require.ErrorContains(t, err, "too large")
}

func TestFetch_redirectOffHostRefused(t *testing.T) {
	other := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("evil"))
	}))
	defer other.Close()
	r := newRelease(t, "v2.1.0")
	r.publish(t, goodEntries("rex_2.1.0_linux_amd64"))
	r.redirect = other.URL
	_, err := fetchAndStage(t, r.source(), "v2.1.0", "linux")
	require.ErrorContains(t, err, "not an allowed download host")
}

func TestFetch_redirectToHTTPRefused(t *testing.T) {
	r := newRelease(t, "v2.1.0")
	r.publish(t, goodEntries("rex_2.1.0_linux_amd64"))
	r.redirect = "http://" + r.host()
	_, err := fetchAndStage(t, r.source(), "v2.1.0", "linux")
	require.ErrorContains(t, err, "https")
}

func TestFetch_redirectOnHostFollowed(t *testing.T) {
	r := newRelease(t, "v2.1.0")
	r.publish(t, goodEntries("rex_2.1.0_linux_amd64"))
	// A redirect to an allowed host is followed (GitHub sends downloads to
	// its object store).
	other := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		name := filepath.Base(req.URL.Path)
		_, _ = w.Write(r.files[name])
	}))
	defer other.Close()
	r.redirect = other.URL
	s := r.source() // httptest servers share one certificate
	u, _ := url.Parse(other.URL)
	s.Hosts = append(s.Hosts, u.Host)
	_, err := fetchAndStage(t, s, "v2.1.0", "linux")
	require.NoError(t, err)
}

func TestFetch_plainHTTPRefused(t *testing.T) {
	s := &Source{API: "http://example.invalid", Download: "http://example.invalid", Repo: "o/rex", Hosts: []string{"example.invalid"}}
	_, err := s.Fetch(context.Background(), "v2.1.0", "linux", "amd64", t.TempDir())
	require.ErrorContains(t, err, "https")
	_, err = s.Latest(context.Background())
	require.ErrorContains(t, err, "https")
}

func TestFetch_badTagRefused(t *testing.T) {
	s := &Source{Repo: "o/rex"}
	for _, tag := range []string{"", "2.1.0", "v2.1.0/../../x", "v2.1.0?x", "latest"} {
		_, err := s.Fetch(context.Background(), tag, "linux", "amd64", t.TempDir())
		require.Error(t, err, tag)
	}
}

func TestFetch_windowsZip(t *testing.T) {
	r := newRelease(t, "v2.1.0")
	name := "rex_2.1.0_windows_amd64.zip"
	r.files[name] = zipOf(t, []entry{
		{name: "rex_2.1.0_windows_amd64/rex.exe", body: "new rex"},
		{name: "rex_2.1.0_windows_amd64/rex-daemon.exe", body: "new daemon"},
		{name: "rex_2.1.0_windows_amd64/LICENSE", body: "MIT"},
	})
	r.files["SHA256SUMS"] = []byte(sumLine(name, r.files[name]))
	dir := t.TempDir()
	// A running rex.exe on Windows can be renamed but not overwritten; the
	// old binary is moved aside to rex.exe.old.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "rex.exe"), []byte("old rex"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "rex.exe.old"), []byte("stale"), 0o755))
	a, err := r.source().Fetch(context.Background(), "v2.1.0", "windows", "amd64", t.TempDir())
	require.NoError(t, err)
	st, err := a.Stage(dir)
	require.NoError(t, err)
	require.NoError(t, st.Install())
	got, err := os.ReadFile(filepath.Join(dir, "rex.exe"))
	require.NoError(t, err)
	require.Equal(t, "new rex", string(got))
	old, err := os.ReadFile(filepath.Join(dir, "rex.exe.old"))
	require.NoError(t, err)
	require.Equal(t, "old rex", string(old))
}

func TestFetch_zipSymlinkAndTraversalRefused(t *testing.T) {
	for _, e := range []entry{
		{name: "rex_2.1.0_windows_amd64/rex.exe", typ: tar.TypeSymlink, body: "C:\\x"},
		{name: "rex_2.1.0_windows_amd64/../rex.exe", body: "x"},
		{name: "rex_2.1.0_windows_amd64\\..\\rex.exe", body: "x"},
	} {
		r := newRelease(t, "v2.1.0")
		name := "rex_2.1.0_windows_amd64.zip"
		r.files[name] = zipOf(t, []entry{e, {name: "rex_2.1.0_windows_amd64/rex-daemon.exe", body: "d"}})
		r.files["SHA256SUMS"] = []byte(sumLine(name, r.files[name]))
		dir, err := fetchAndStage(t, r.source(), "v2.1.0", "windows")
		require.Error(t, err, e.name)
		requireEmptyDir(t, dir)
	}
}

func TestStage_cleanupOnAbort(t *testing.T) {
	r := newRelease(t, "v2.1.0")
	r.publish(t, goodEntries("rex_2.1.0_linux_amd64"))
	a, err := r.source().Fetch(context.Background(), "v2.1.0", "linux", "amd64", t.TempDir())
	require.NoError(t, err)
	dir := t.TempDir()
	st, err := a.Stage(dir)
	require.NoError(t, err)
	st.Cleanup()
	requireEmptyDir(t, dir)
}

func requireEmptyDir(t *testing.T, dir string) {
	t.Helper()
	ents, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Empty(t, ents, "nothing may be written to the install dir")
}

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v2.0.0", "v2.0.0", 0},
		{"v2.0.0", "v2.0.1", -1},
		{"v2.1.0", "v2.0.9", 1},
		{"v10.0.0", "v9.0.0", 1},
		{"v2.0.0-rc.1", "v2.0.0", -1},
		{"v2.0.0-rc.2", "v2.0.0-rc.10", -1},
		{"v2.0.0-alpha", "v2.0.0-alpha.1", -1},
		{"v2.0.0+meta", "v2.0.0", 0},
	}
	for _, c := range cases {
		got, err := Compare(c.a, c.b)
		require.NoError(t, err, c)
		require.Equal(t, c.want, got, c)
	}
	_, err := Compare("dev", "v2.0.0")
	require.Error(t, err)
}

func TestKnown(t *testing.T) {
	require.True(t, Known("v2.0.0"))
	require.True(t, Known("v2.0.0-rc.1"))
	require.False(t, Known("dev"))
	require.False(t, Known(""))
	require.False(t, Known("v0.0.0-20261112093000-abcdef123456"))
	require.False(t, Known("v2.0.1-0.20261112093000-abcdef123456"))
	require.False(t, Known("v2.0.0-rc.1.0.20261112093000-abcdef123456+dirty"))
	require.False(t, Known("2.0.0"))
}

func TestNormalizeTag(t *testing.T) {
	tag, err := NormalizeTag("2.1.0")
	require.NoError(t, err)
	require.Equal(t, "v2.1.0", tag)
	_, err = NormalizeTag("v2.1")
	require.Error(t, err)
	_, err = NormalizeTag("v2.1.0;rm")
	require.Error(t, err)
}
