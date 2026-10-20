package scaffold

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Repository is the Git repository the Howdy packages are released from.
const Repository = "https://github.com/howdy-gleam/howdy.git"

// packageDirs maps each Howdy package to its directory in Repository: ""
// for the core package at the root.
var packageDirs = map[string]string{
	"howdy":               "",
	"howdy_admin":         "admin",
	"howdy_auth":          "auth",
	"howdy_cache":         "cache",
	"howdy_database":      "database",
	"howdy_dev":           "howdy_dev",
	"howdy_flags":         "flags",
	"howdy_i18n":          "i18n",
	"howdy_jobs":          "jobs",
	"howdy_mail":          "mail",
	"howdy_notifications": "notifications",
	"howdy_openapi":       "openapi",
	"howdy_remote":        "remote",
	"howdy_storage":       "storage",
	"howdy_telemetry":     "telemetry",
	"howdy_ui":            "ui",
}

// SourceKind is where a project downloads its Howdy packages from.
type SourceKind string

const (
	// Git takes every package from a release tag of Repository.
	Git SourceKind = "git"
	// Hex takes every package from Hex, by the version it has in the
	// release.
	Hex SourceKind = "hex"
)

// DefaultSource is where new projects get their Howdy packages unless
// --source says otherwise.
const DefaultSource = Hex

// ParseSource reads the value of the --source flag.
func ParseSource(s string) (SourceKind, error) {
	switch kind := SourceKind(strings.ToLower(strings.TrimSpace(s))); kind {
	case Git, Hex:
		return kind, nil
	}
	return "", fmt.Errorf("unknown source %q: use git or hex", s)
}

// releasesJSON is a copy of releases/releases.json in the Howdy repository:
// the version of every package in each Howdy release, which a project
// taking its packages from Hex needs. Update it with Release; a release
// newer than this copy is read from its tag instead (see releasePackages).
//
//go:embed releases.json
var releasesJSON []byte

// releases maps each Howdy release in releasesJSON to the versions of its
// packages.
var releases = func() map[string]map[string]string {
	byVersion, err := parseReleases(releasesJSON)
	if err != nil {
		panic("releases.json: " + err.Error())
	}
	return byVersion
}()

// parseReleases reads a releases.json, mapping each release to the versions
// of its packages.
func parseReleases(data []byte) (map[string]map[string]string, error) {
	var file struct {
		Releases []struct {
			Version  string            `json:"version"`
			Packages map[string]string `json:"packages"`
		} `json:"releases"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, err
	}
	byVersion := map[string]map[string]string{}
	for _, r := range file.Releases {
		byVersion[r.Version] = r.Packages
	}
	return byVersion, nil
}

// ReleasesURL is where releases.json is read from for a release newer than
// the copy built into howdy: the file at the release's tag, %s, which
// scripts/release.sh commits before tagging. Tests point it at a local
// server.
var ReleasesURL = "https://raw.githubusercontent.com/howdy-gleam/howdy/%s/releases/releases.json"

var (
	fetchedMu sync.Mutex
	// fetched holds the releases read from GitHub, so each is read once.
	fetched = map[string]map[string]string{}
)

// releasePackages is the version of every package in a Howdy release, such
// as "1.0.0": from the copy of releases.json built into howdy, or for a
// newer release, from releases.json at its tag on GitHub.
func releasePackages(release string) (map[string]string, error) {
	if packages, ok := releases[release]; ok {
		return packages, nil
	}
	fetchedMu.Lock()
	defer fetchedMu.Unlock()
	if packages, ok := fetched[release]; ok {
		return packages, nil
	}
	packages, err := fetchRelease(release)
	if err != nil {
		return nil, fmt.Errorf("this howdy does not know the package versions of Howdy %s on Hex, and %w; 'howdy upgrade' gets a newer howdy", release, err)
	}
	fetched[release] = packages
	return packages, nil
}

func fetchRelease(release string) (map[string]string, error) {
	url := fmt.Sprintf(ReleasesURL, "v"+release)
	client := http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("could not read them from GitHub: %w", err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, fmt.Errorf("github.com/howdy-gleam/howdy has no release tag v%s to read them from", release)
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("could not read them from GitHub: GET %s: %s", url, resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("could not read them from GitHub: %w", err)
	}
	byVersion, err := parseReleases(body)
	if err != nil {
		return nil, fmt.Errorf("could not read releases.json at tag v%s: %w", release, err)
	}
	packages, ok := byVersion[release]
	if !ok {
		return nil, fmt.Errorf("releases.json at tag v%s does not list it", release)
	}
	return packages, nil
}

// Source is where a project takes its Howdy packages from. Every Howdy
// package in a project comes from the same source and release, so they
// always match.
type Source struct {
	Kind SourceKind
	// Git and Ref are the repository and its ref, such as the release tag
	// "v1.0.0", for a Git source.
	Git, Ref string
	// Release is the Howdy release, such as "1.0.0", whose package versions
	// a Hex source uses.
	Release string
	// packages is the version of each package in Release, for Hex.
	packages map[string]string
}

// ReleaseSource is the source of the packages of a Howdy release, such as
// "1.0.0", from kind. For Hex it reads the release's package versions from
// GitHub when it is newer than the copy of releases.json built into howdy.
func ReleaseSource(kind SourceKind, release string) (Source, error) {
	if kind != Hex {
		return Source{Kind: Git, Git: Repository, Ref: "v" + release}, nil
	}
	packages, err := releasePackages(release)
	if err != nil {
		return Source{}, err
	}
	return Source{Kind: Hex, Release: release, packages: packages}, nil
}

// Check fails for a package the source does not have: a Hex source has
// only the packages of its release.
func (s Source) Check(pkg string) error {
	if _, ok := s.packages[pkg]; s.Kind == Hex && !ok {
		return fmt.Errorf("Howdy %s has no %s", s.Release, pkg)
	}
	return nil
}

// Version is the version of the Howdy package pkg the source gives: the
// ref for Git, or its version in the release for Hex.
func (s Source) Version(pkg string) string {
	if s.Kind == Hex {
		return s.packages[pkg]
	}
	return s.Ref
}

// Dependency is the gleam.toml line for the Howdy package pkg, without a
// newline. From Git it is howdy_ui = { git = "...", ref = "v1.0.0", path =
// "ui" }; from Hex, howdy_ui = ">= 1.0.0 and < 2.0.0", the package's
// version in the release up to its next major. It panics for a package that
// is not a Howdy package or that the source does not have (see Check).
func (s Source) Dependency(pkg string) string {
	dir, ok := packageDirs[pkg]
	if !ok {
		panic(fmt.Sprintf("%s is not a Howdy package", pkg))
	}
	if err := s.Check(pkg); err != nil {
		panic(err.Error())
	}
	switch {
	case s.Kind == Hex:
		version := s.Version(pkg)
		major, _ := strconv.Atoi(strings.SplitN(version, ".", 2)[0])
		return fmt.Sprintf("%s = \">= %s and < %d.0.0\"", pkg, version, major+1)
	case dir == "":
		return fmt.Sprintf("%s = { git = %q, ref = %q }", pkg, s.Git, s.Ref)
	}
	return fmt.Sprintf("%s = { git = %q, ref = %q, path = %q }", pkg, s.Git, s.Ref, dir)
}
