// Package selfupdate downloads, verifies and installs rex release builds
// from GitHub releases (rex update). It is the Go side of install.sh and
// install.ps1 and shares no code with them.
//
// Safety rules, each with a test:
//   - every request is https to an allowed host, and so is every redirect;
//   - the archive must match its line in the release's SHA256SUMS;
//   - only rex and rex-daemon are extracted, from the archive's top folder,
//     as regular files of bounded size; a link, a path outside that
//     folder or an oversized entry rejects the whole archive;
//   - new binaries are written next to the old ones as temp files, synced,
//     and renamed over them only after both are complete.
//
// SHA256SUMS comes from the same release as the archive, so the check
// catches a corrupt or truncated download, not a compromised release.
package selfupdate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Defaults for a Source left at its zero values.
const (
	DefaultAPI      = "https://api.github.com"
	DefaultDownload = "https://github.com"
	DefaultRepo     = "TristanBietsch/rex"

	// DefaultMaxArchive caps a downloaded archive.
	DefaultMaxArchive = 256 << 20
	// DefaultMaxEntry caps one extracted file.
	DefaultMaxEntry = 256 << 20

	maxSums     = 64 << 10
	maxAPIBody  = 4 << 20
	maxEntries  = 64
	maxRedirect = 10
)

// DefaultHosts are the hosts rex downloads from: the API, the release
// download URL and the object stores GitHub redirects release assets to.
var DefaultHosts = []string{
	"api.github.com",
	"github.com",
	"objects.githubusercontent.com",
	"release-assets.githubusercontent.com",
}

// Source is where releases come from. The zero value is GitHub.
type Source struct {
	// HTTP is the client to use; nil means one with TLS 1.2+ and the
	// proxy from the environment. Its CheckRedirect is replaced.
	HTTP *http.Client
	// API is the GitHub API base URL.
	API string
	// Download is the base of <Download>/<Repo>/releases/download/<tag>/<file>.
	Download string
	// Repo is owner/name.
	Repo string
	// Token, when set, is sent to the API (raises the rate limit). It is
	// never sent to a download host.
	Token string
	// Hosts lists the hosts requests and redirects may go to: a bare host
	// name allows the default https port, host:port allows that port.
	Hosts []string
	// MaxArchive and MaxEntry cap the archive and one extracted file.
	MaxArchive, MaxEntry int64
}

func (s *Source) api() string      { return orDefault(s.API, DefaultAPI) }
func (s *Source) download() string { return orDefault(s.Download, DefaultDownload) }
func (s *Source) repo() string     { return orDefault(s.Repo, DefaultRepo) }

func (s *Source) hosts() []string {
	if len(s.Hosts) == 0 {
		return DefaultHosts
	}
	return s.Hosts
}

func (s *Source) maxArchive() int64 { return orDefaultN(s.MaxArchive, DefaultMaxArchive) }
func (s *Source) maxEntry() int64   { return orDefaultN(s.MaxEntry, DefaultMaxEntry) }

func orDefault(v, d string) string {
	if v == "" {
		return d
	}
	return strings.TrimRight(v, "/")
}

func orDefaultN(v, d int64) int64 {
	if v <= 0 {
		return d
	}
	return v
}

// checkURL refuses anything but https to an allowed host.
func (s *Source) checkURL(u *url.URL) error {
	if u.Scheme != "https" {
		return fmt.Errorf("refusing non-https URL %s", u.Redacted())
	}
	for _, h := range s.hosts() {
		if h == u.Host || (h == u.Hostname() && (u.Port() == "" || u.Port() == "443")) {
			return nil
		}
	}
	return fmt.Errorf("%s is not an allowed download host", u.Host)
}

func (s *Source) client() *http.Client {
	var c http.Client
	if s.HTTP != nil {
		c = *s.HTTP
	} else {
		t := http.DefaultTransport.(*http.Transport).Clone()
		t.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
		c.Transport = t
	}
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirect {
			return errors.New("too many redirects")
		}
		if err := s.checkURL(req.URL); err != nil {
			return fmt.Errorf("redirect: %w", err)
		}
		// Credentials never follow a redirect.
		req.Header.Del("Authorization")
		return nil
	}
	return &c
}

// get fetches rawURL into w, failing past max bytes.
func (s *Source) get(ctx context.Context, rawURL string, api bool, maxBytes int64, w io.Writer) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	if err := s.checkURL(u); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "rex-updater")
	if api {
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		if s.Token != "" {
			req.Header.Set("Authorization", "Bearer "+s.Token)
		}
	}
	resp, err := s.client().Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", u.Redacted(), resp.Status)
	}
	if resp.ContentLength > maxBytes {
		return fmt.Errorf("GET %s: too large (%d bytes, limit %d)", u.Redacted(), resp.ContentLength, maxBytes)
	}
	n, err := io.Copy(w, io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return fmt.Errorf("GET %s: %w", u.Redacted(), err)
	}
	if n > maxBytes {
		return fmt.Errorf("GET %s: too large (over %d bytes)", u.Redacted(), maxBytes)
	}
	return nil
}

// Latest returns the tag of the latest release.
func (s *Source) Latest(ctx context.Context) (string, error) {
	var buf bytes.Buffer
	if err := s.get(ctx, s.api()+"/repos/"+s.repo()+"/releases/latest", true, maxAPIBody, &buf); err != nil {
		return "", err
	}
	var rel struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(buf.Bytes(), &rel); err != nil {
		return "", fmt.Errorf("latest release: %w", err)
	}
	if err := checkTag(rel.TagName); err != nil {
		return "", err
	}
	return rel.TagName, nil
}

// checkTag accepts only a tag NormalizeTag would produce unchanged.
func checkTag(tag string) error {
	if !strings.HasPrefix(tag, "v") {
		return fmt.Errorf("unexpected tag %q", tag)
	}
	n, err := NormalizeTag(tag)
	if err != nil {
		return err
	}
	if n != tag {
		return fmt.Errorf("unexpected tag %q", tag)
	}
	return nil
}

// Archive is a downloaded release archive whose checksum matched.
type Archive struct {
	// Path is the local file, in the tmpDir given to Fetch.
	Path string
	// Name is the asset name, rex_<ver>_<os>_<arch>.tar.gz or .zip.
	Name string
	// Tag is the release tag.
	Tag string
	// SHA256 is the verified digest, hex.
	SHA256 string
	goos   string
	dir    string // top folder inside the archive
	limit  int64
}

// AssetName is the archive name for a release tag and platform.
func AssetName(tag, goos, goarch string) string {
	ext := ".tar.gz"
	if goos == "windows" {
		ext = ".zip"
	}
	return "rex_" + strings.TrimPrefix(tag, "v") + "_" + goos + "_" + goarch + ext
}

// Fetch downloads the release's SHA256SUMS and the archive for goos/goarch
// into tmpDir and verifies the archive against its checksum line.
func (s *Source) Fetch(ctx context.Context, tag, goos, goarch, tmpDir string) (*Archive, error) {
	if err := checkTag(tag); err != nil {
		return nil, err
	}
	base := s.download() + "/" + s.repo() + "/releases/download/" + tag + "/"
	var sums bytes.Buffer
	if err := s.get(ctx, base+"SHA256SUMS", false, maxSums, &sums); err != nil {
		return nil, fmt.Errorf("release %s: %w", tag, err)
	}
	name := AssetName(tag, goos, goarch)
	want, err := sumFor(sums.String(), name)
	if err != nil {
		return nil, fmt.Errorf("release %s has no build for %s/%s: %w", tag, goos, goarch, err)
	}

	f, err := os.CreateTemp(tmpDir, "rex-download-*")
	if err != nil {
		return nil, err
	}
	keep := false
	defer func() {
		_ = f.Close()
		if !keep {
			_ = os.Remove(f.Name())
		}
	}()
	h := sha256.New()
	if err := s.get(ctx, base+name, false, s.maxArchive(), io.MultiWriter(f, h)); err != nil {
		return nil, err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if got != want {
		return nil, fmt.Errorf("checksum mismatch for %s (want %s, got %s); nothing was installed", name, want, got)
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	keep = true
	return &Archive{
		Path: f.Name(), Name: name, Tag: tag, SHA256: got,
		goos: goos, dir: strings.TrimSuffix(strings.TrimSuffix(name, ".zip"), ".tar.gz"),
		limit: s.maxEntry(),
	}, nil
}

// Remove deletes the downloaded file.
func (a *Archive) Remove() { _ = os.Remove(a.Path) }

// sumFor finds name's digest in a SHA256SUMS file ("<hex>  <name>" or
// "<hex> *<name>" per line). A name listed twice is an error.
func sumFor(sums, name string) (string, error) {
	found := ""
	for _, line := range strings.Split(sums, "\n") {
		line = strings.TrimRight(line, "\r")
		digest, file, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		file = strings.TrimPrefix(strings.TrimPrefix(file, " "), "*")
		if file != name {
			continue
		}
		if found != "" {
			return "", fmt.Errorf("%s listed more than once in SHA256SUMS", name)
		}
		digest = strings.ToLower(digest)
		if len(digest) != 64 {
			return "", fmt.Errorf("bad SHA256SUMS line for %s", name)
		}
		if _, err := hex.DecodeString(digest); err != nil {
			return "", fmt.Errorf("bad SHA256SUMS line for %s", name)
		}
		found = digest
	}
	if found == "" {
		return "", fmt.Errorf("%s is not in SHA256SUMS", name)
	}
	return found, nil
}

// DefaultTimeout bounds a version check.
const DefaultTimeout = 10 * time.Second
