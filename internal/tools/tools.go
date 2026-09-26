// Package tools finds and runs the external programs a Howdy project needs.
package tools

import (
	"bytes"
	"fmt"
	"os/exec"
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
