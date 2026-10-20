package scaffold

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Repository is the Git repository the Howdy packages are released from.
const Repository = "https://github.com/mikeyjones/howdy.git"

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
// --source says otherwise. Switch it to Hex once the packages of Release
// are published there.
const DefaultSource = Git

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
// taking its packages from Hex needs. Update it with Release.
//
//go:embed releases.json
var releasesJSON []byte

// releases maps each Howdy release to the versions of its packages.
var releases = func() map[string]map[string]string {
	var file struct {
		Releases []struct {
			Version  string            `json:"version"`
			Packages map[string]string `json:"packages"`
		} `json:"releases"`
	}
	if err := json.Unmarshal(releasesJSON, &file); err != nil {
		panic("releases.json: " + err.Error())
	}
	byVersion := map[string]map[string]string{}
	for _, r := range file.Releases {
		byVersion[r.Version] = r.Packages
	}
	return byVersion
}()

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
}

// ReleaseSource is the source of the packages of a Howdy release, such as
// "1.0.0", from kind. Hex knows only the releases in releases.json.
func ReleaseSource(kind SourceKind, release string) (Source, error) {
	if kind != Hex {
		return Source{Kind: Git, Git: Repository, Ref: "v" + release}, nil
	}
	if _, ok := releases[release]; !ok {
		return Source{}, fmt.Errorf("this howdy does not know the package versions of Howdy %s on Hex; 'howdy upgrade' gets a newer howdy", release)
	}
	return Source{Kind: Hex, Release: release}, nil
}

// Check fails for a package the source does not have: a Hex source has
// only the packages of its release.
func (s Source) Check(pkg string) error {
	if _, ok := releases[s.Release][pkg]; s.Kind == Hex && !ok {
		return fmt.Errorf("Howdy %s has no %s", s.Release, pkg)
	}
	return nil
}

// Version is the version of the Howdy package pkg the source gives: the
// ref for Git, or its version in the release for Hex.
func (s Source) Version(pkg string) string {
	if s.Kind == Hex {
		return releases[s.Release][pkg]
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
