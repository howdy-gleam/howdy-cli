// Package upgrade replaces the running howdy with a release from GitHub.
//
// Releases are made by GoReleaser (.goreleaser.yaml) when a v* tag is
// pushed: each has an archive per platform, named by ArchiveName, holding
// the howdy binary, and a checksums.txt with the SHA-256 of every archive.
package upgrade

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Repo is the GitHub repository whose releases howdy upgrades to. It can be
// set at build time with -ldflags "-X howdy/internal/upgrade.Repo=owner/name".
var Repo = "mikeyjones/howdy_cli"

// checksums is the name of the release asset holding the archives' SHA-256s.
const checksums = "checksums.txt"

// Release is a GitHub release.
type Release struct {
	Tag    string  `json:"tag_name"`
	URL    string  `json:"html_url"`
	Assets []Asset `json:"assets"`
}

// Version is the release's version without the tag's leading v.
func (r Release) Version() string {
	return strings.TrimPrefix(r.Tag, "v")
}

// Asset is a file attached to a release.
type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

func (r Release) asset(name string) (Asset, bool) {
	for _, a := range r.Assets {
		if a.Name == name {
			return a, true
		}
	}
	return Asset{}, false
}

// Client reads releases from the GitHub API.
type Client struct {
	HTTP *http.Client
	// API is the GitHub API's base URL; tests point it at a local server.
	API  string
	Repo string
	// Token, when set, is sent to the GitHub API to avoid the rate limit on
	// anonymous requests.
	Token string
}

// NewClient is a client for Repo, using GITHUB_TOKEN when it is set.
func NewClient() Client {
	return Client{
		HTTP:  &http.Client{Timeout: 2 * time.Minute},
		API:   "https://api.github.com",
		Repo:  Repo,
		Token: os.Getenv("GITHUB_TOKEN"),
	}
}

// Latest is the newest published release, leaving out drafts and
// pre-releases.
func (c Client) Latest(ctx context.Context) (Release, error) {
	return c.release(ctx, "releases/latest")
}

// Tagged is the release of version, such as 1.2.0, with or without a
// leading v.
func (c Client) Tagged(ctx context.Context, version string) (Release, error) {
	return c.release(ctx, "releases/tags/v"+strings.TrimPrefix(version, "v"))
}

func (c Client) release(ctx context.Context, endpoint string) (Release, error) {
	url := fmt.Sprintf("%s/repos/%s/%s", strings.TrimSuffix(c.API, "/"), c.Repo, endpoint)
	body, err := c.get(ctx, url, "application/vnd.github+json")
	if err != nil {
		var status statusError
		if errors.As(err, &status) && status.code == http.StatusNotFound {
			if endpoint == "releases/latest" {
				return Release{}, fmt.Errorf("github.com/%s has no published releases yet", c.Repo)
			}
			return Release{}, fmt.Errorf("github.com/%s has no release %s", c.Repo, strings.TrimPrefix(endpoint, "releases/tags/"))
		}
		return Release{}, err
	}
	var r Release
	if err := json.Unmarshal(body, &r); err != nil {
		return Release{}, fmt.Errorf("could not read the release from GitHub: %w", err)
	}
	return r, nil
}

type statusError struct {
	url  string
	code int
}

func (e statusError) Error() string {
	msg := fmt.Sprintf("GET %s: %d %s", e.url, e.code, http.StatusText(e.code))
	if e.code == http.StatusForbidden || e.code == http.StatusTooManyRequests {
		msg += "\n  GitHub may be rate limiting you; set GITHUB_TOKEN and try again"
	}
	return msg
}

func (c Client) get(ctx context.Context, url, accept string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("User-Agent", "howdy-cli")
	if c.Token != "" && strings.HasPrefix(url, strings.TrimSuffix(c.API, "/")) {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach GitHub: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, statusError{url, resp.StatusCode}
	}
	return io.ReadAll(resp.Body)
}

// ArchiveName is the release asset holding howdy for a platform, as
// GoReleaser names it.
func ArchiveName(goos, goarch string) string {
	if goos == "windows" {
		return fmt.Sprintf("howdy_%s_%s.zip", goos, goarch)
	}
	return fmt.Sprintf("howdy_%s_%s.tar.gz", goos, goarch)
}

// Download fetches the release's archive for this platform, checks it
// against the release's checksums and returns the howdy binary inside.
func (c Client) Download(ctx context.Context, r Release) ([]byte, error) {
	return c.download(ctx, r, runtime.GOOS, runtime.GOARCH)
}

func (c Client) download(ctx context.Context, r Release, goos, goarch string) ([]byte, error) {
	name := ArchiveName(goos, goarch)
	archive, ok := r.asset(name)
	if !ok {
		return nil, fmt.Errorf("release %s has no build for %s/%s (no %s)", r.Tag, goos, goarch, name)
	}
	sums, ok := r.asset(checksums)
	if !ok {
		return nil, fmt.Errorf("release %s has no %s to verify the download with", r.Tag, checksums)
	}
	list, err := c.get(ctx, sums.URL, "application/octet-stream")
	if err != nil {
		return nil, err
	}
	want, err := checksumOf(list, name)
	if err != nil {
		return nil, fmt.Errorf("release %s: %w", r.Tag, err)
	}
	data, err := c.get(ctx, archive.URL, "application/octet-stream")
	if err != nil {
		return nil, err
	}
	got := sha256.Sum256(data)
	if hex.EncodeToString(got[:]) != want {
		return nil, fmt.Errorf("%s does not match its checksum in %s; nothing was changed", name, checksums)
	}
	binary := "howdy"
	if goos == "windows" {
		binary += ".exe"
	}
	if strings.HasSuffix(name, ".zip") {
		return fromZip(data, binary)
	}
	return fromTarGz(data, binary)
}

// checksumOf finds name's SHA-256 in a checksums.txt, whose lines are
// "<hex>  <file>".
func checksumOf(list []byte, name string) (string, error) {
	scanner := bufio.NewScanner(bytes.NewReader(list))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == name {
			return strings.ToLower(fields[0]), nil
		}
	}
	return "", fmt.Errorf("%s has no checksum for %s", checksums, name)
}

func fromTarGz(data []byte, binary string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("could not open the archive: %w", err)
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("could not read the archive: %w", err)
		}
		if h.Typeflag == tar.TypeReg && path.Base(h.Name) == binary {
			return io.ReadAll(tr)
		}
	}
	return nil, fmt.Errorf("the archive has no %s", binary)
}

func fromZip(data []byte, binary string) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("could not open the archive: %w", err)
	}
	for _, f := range zr.File {
		if !f.FileInfo().IsDir() && path.Base(f.Name) == binary {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(rc)
		}
	}
	return nil, fmt.Errorf("the archive has no %s", binary)
}

// Executable is the path of the running howdy, with symlinks resolved so
// the file itself is replaced rather than a link to it.
func Executable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("could not find the running howdy: %w", err)
	}
	return filepath.EvalSymlinks(exe)
}

// Replace swaps the file at exe for binary. The new binary is written next
// to it and renamed over it, so exe is never left half-written.
func Replace(exe string, binary []byte) error {
	dir := filepath.Dir(exe)
	tmp, err := os.CreateTemp(dir, ".howdy-upgrade-*")
	if err != nil {
		return writeError(dir, err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(binary); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	mode := os.FileMode(0o755)
	if info, err := os.Stat(exe); err == nil {
		mode = info.Mode().Perm() | 0o111
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		// Windows will not replace a running executable, but will rename it.
		old := exe + ".old"
		_ = os.Remove(old)
		if err := os.Rename(exe, old); err != nil {
			return writeError(dir, err)
		}
		if err := os.Rename(tmp.Name(), exe); err != nil {
			_ = os.Rename(old, exe)
			return writeError(dir, err)
		}
		return nil
	}
	if err := os.Rename(tmp.Name(), exe); err != nil {
		return writeError(dir, err)
	}
	return nil
}

func writeError(dir string, err error) error {
	if errors.Is(err, os.ErrPermission) {
		return fmt.Errorf("howdy is installed in %s, which you cannot write to.\n  Run the upgrade again with the permissions to change it, such as with sudo", dir)
	}
	return fmt.Errorf("could not replace howdy in %s: %w", dir, err)
}

// Compare orders two versions such as 1.2.0, v1.10.0 or 1.3.0-rc.1: it is
// negative when a is older than b, zero when they are the same and
// positive when a is newer. A pre-release is older than its release.
func Compare(a, b string) (int, error) {
	ac, ap, err := parse(a)
	if err != nil {
		return 0, err
	}
	bc, bp, err := parse(b)
	if err != nil {
		return 0, err
	}
	for i := range ac {
		if ac[i] != bc[i] {
			if ac[i] < bc[i] {
				return -1, nil
			}
			return 1, nil
		}
	}
	switch {
	case ap == bp:
		return 0, nil
	case ap == "":
		return 1, nil
	case bp == "":
		return -1, nil
	}
	return strings.Compare(ap, bp), nil
}

func parse(v string) (core [3]int, pre string, err error) {
	s := strings.TrimPrefix(strings.TrimSpace(v), "v")
	s, _, _ = strings.Cut(s, "+")
	s, pre, _ = strings.Cut(s, "-")
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return core, "", fmt.Errorf("%q is not a version such as 1.2.0", v)
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return core, "", fmt.Errorf("%q is not a version such as 1.2.0", v)
		}
		core[i] = n
	}
	return core, pre, nil
}
