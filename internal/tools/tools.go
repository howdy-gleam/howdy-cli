// Package tools finds and runs the external programs a Howdy project needs.
package tools

import (
	"bytes"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// Tool is an executable found on PATH.
type Tool struct {
	Name    string
	Path    string
	Version string
}

// FindGleam locates the gleam compiler and reads its version.
func FindGleam() (Tool, error) {
	path, err := exec.LookPath("gleam")
	if err != nil {
		return Tool{}, fmt.Errorf("gleam is not installed or not on your PATH.\n  Install it from https://gleam.run/getting-started/installing/ and run this command again")
	}
	out, err := exec.Command(path, "--version").Output()
	if err != nil {
		return Tool{}, fmt.Errorf("found gleam at %s but 'gleam --version' failed: %w", path, err)
	}
	return Tool{Name: "gleam", Path: path, Version: strings.TrimSpace(string(out))}, nil
}

// FindErlang locates the Erlang runtime, which Gleam needs for the erlang target.
func FindErlang() (Tool, error) {
	path, err := exec.LookPath("erl")
	if err != nil {
		return Tool{}, fmt.Errorf("erl is not on your PATH")
	}
	return Tool{Name: "erl", Path: path}, nil
}

var gleamRelease = regexp.MustCompile(`^gleam ([0-9]+\.[0-9]+\.[0-9]+)$`)

// GleamRelease is the release of a gleam found by FindGleam, such as
// "1.19.2", or "" for a build that is not a release.
func (t Tool) GleamRelease() string {
	if m := gleamRelease.FindStringSubmatch(t.Version); m != nil {
		return m[1]
	}
	return ""
}

var otpRelease = regexp.MustCompile(`^[0-9]+$`)

// ErlangRelease is the major release of the Erlang/OTP on PATH, such as
// "29", or "" if there is none.
func ErlangRelease() string {
	path, err := exec.LookPath("erl")
	if err != nil {
		return ""
	}
	out, err := exec.Command(path, "-noshell", "-eval",
		"io:put_chars(erlang:system_info(otp_release)), halt().").Output()
	if release := strings.TrimSpace(string(out)); err == nil && otpRelease.MatchString(release) {
		return release
	}
	return ""
}

// Run executes the tool in dir and returns its combined output in the error on failure.
func (t Tool) Run(dir string, args ...string) error {
	cmd := exec.Command(t.Path, args...)
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s failed: %w\n%s", t.Name, strings.Join(args, " "), err, strings.TrimSpace(out.String()))
	}
	return nil
}

// Output executes the tool in dir and returns its combined output, which
// is wanted whether it succeeds, as with the compiler's warnings, or not.
func (t Tool) Output(dir string, args ...string) (string, error) {
	cmd := exec.Command(t.Path, args...)
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return strings.TrimSpace(out.String()), err
}
