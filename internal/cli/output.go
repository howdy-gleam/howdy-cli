package cli

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"howdy/internal/tui"
)

// output is how a command that changes a project reports: animated or
// plain progress and closing notes for people, or with --json one JSON
// object for programs. With --dry-run the command works on a copy of the
// project and reports what it would create and change, writing nothing.
type output struct {
	plain, json, dryRun bool

	// dir is the project the command works on: the real one, or with
	// --dry-run a copy in a temporary directory, which cleanup removes.
	dir, copy string
	// before holds a hash of every file in dir before the command ran, to
	// tell what it created and changed.
	before   map[string][32]byte
	warnings []string
}

// result is what --json prints.
type result struct {
	Command string `json:"command"`
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
	DryRun  bool   `json:"dry_run,omitempty"`
	// Created and Modified are paths relative to the project.
	Created  []string `json:"created"`
	Modified []string `json:"modified"`
	// ManualSteps are what the command could not do itself, such as
	// registering a controller in a file whose shape it did not recognise.
	ManualSteps []string      `json:"manual_steps"`
	Warnings    []string      `json:"warnings"`
	Next        []nextCommand `json:"next"`
	Notes       []string      `json:"notes"`
}

type nextCommand struct {
	Command string `json:"command"`
	Note    string `json:"note"`
}

// flags adds --plain and --json, and --dry-run when the command offers it.
func (o *output) flags(cmd *cobra.Command, dryRun bool) {
	cmd.Flags().BoolVar(&o.plain, "plain", false, "print progress as plain lines instead of animating")
	cmd.Flags().BoolVar(&o.json, "json", false, "print the result as one JSON object: the files created and modified, steps left to do by hand, and what to do next")
	if dryRun {
		cmd.Flags().BoolVar(&o.dryRun, "dry-run", false, "show which files would be created and modified, without changing the project")
	}
}

// projectDir finds the enclosing project and returns the directory the
// command should work in: the project, or with --dry-run a copy of it.
func (o *output) projectDir() (string, error) {
	dir, err := findProjectDir()
	if err != nil {
		return "", err
	}
	if o.dryRun {
		if o.copy, err = os.MkdirTemp("", "howdy-dry-run-"); err != nil {
			return "", err
		}
		if err := copyProject(dir, o.copy); err != nil {
			return "", err
		}
		dir = o.copy
	}
	o.dir = dir
	if o.before, err = snapshot(dir); err != nil {
		return "", err
	}
	return dir, nil
}

// cleanup removes the copy a dry run worked on.
func (o *output) cleanup() {
	if o.copy != "" {
		_ = os.RemoveAll(o.copy)
	}
}

// skipDeps is the Skip of a step that downloads dependencies: skipped with
// --skip-deps, and in a dry run, which must not touch the network.
func (o *output) skipDeps(skip bool) func() (string, bool) {
	return func() (string, bool) {
		switch {
		case o.dryRun:
			return "skipped (--dry-run)", true
		case skip:
			return "skipped (--skip-deps)", true
		}
		return "", false
	}
}

// run executes the steps, showing their progress unless the output is JSON.
func (o *output) run(title string, steps []tui.Step) error {
	if o.json {
		return tui.RunSilent(steps, (*silentReporter)(o))
	}
	return tui.Run(title, steps, o.plain)
}

type silentReporter output

func (*silentReporter) Detail(string)      {}
func (*silentReporter) Log(string)         {}
func (r *silentReporter) Warn(line string) { r.warnings = append(r.warnings, line) }

// done reports how the command went: the files it created and modified,
// the manual steps it left, and what to do next.
func (o *output) done(command, name string, commands []tui.Command, notes, manual []string) error {
	created, modified, err := o.changes()
	if err != nil {
		return err
	}
	return o.report(command, name, created, modified, commands, notes, manual)
}

// report is done for a command that knows what it created and modified.
func (o *output) report(command, name string, created, modified []string, commands []tui.Command, notes, manual []string) error {
	if o.json {
		r := result{
			Command: command, OK: true, DryRun: o.dryRun,
			Created: nonNil(created), Modified: nonNil(modified),
			ManualSteps: nonNil(manual), Warnings: nonNil(o.warnings), Notes: nonNil(notes),
			Next: []nextCommand{},
		}
		if !o.dryRun {
			for _, c := range commands {
				r.Next = append(r.Next, nextCommand{c.Cmd, c.Note})
			}
		}
		return printJSON(os.Stdout, r)
	}
	if o.dryRun {
		fmt.Print(tui.DryRun(created, modified, manual))
		return nil
	}
	lines := make([]string, 0, len(manual)+len(notes))
	for _, m := range manual {
		lines = append(lines, "Needs a hand: "+m)
	}
	fmt.Print(tui.NextSteps(name, commands, append(lines, notes...)))
	return nil
}

// changes compares the project with the snapshot taken before the command.
func (o *output) changes() (created, modified []string, err error) {
	created, modified = []string{}, []string{}
	if o.dir == "" {
		return created, modified, nil
	}
	after, err := snapshot(o.dir)
	if err != nil {
		return nil, nil, err
	}
	for path, sum := range after {
		before, ok := o.before[path]
		switch {
		case !ok:
			created = append(created, path)
		case before != sum:
			modified = append(modified, path)
		}
	}
	sort.Strings(created)
	sort.Strings(modified)
	return created, modified, nil
}

// failed prints a command's error as JSON, for Execute when --json is set.
func failed(w io.Writer, command string, err error) {
	_ = printJSON(w, result{
		Command: command, Error: err.Error(),
		Created: []string{}, Modified: []string{}, ManualSteps: []string{},
		Warnings: []string{}, Next: []nextCommand{}, Notes: []string{},
	})
}

func printJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// skipped are the folders of a project that belong to tools rather than to
// the project's sources: build output, dependencies and version control.
var skipped = map[string]bool{"build": true, ".git": true, "tmp": true, "node_modules": true}

// projectFiles calls each for every regular file of the project under dir,
// with its path relative to dir.
func projectFiles(dir string, each func(rel, path string) error) error {
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != dir && skipped[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		return each(filepath.ToSlash(rel), path)
	})
}

// snapshot hashes every file of the project.
func snapshot(dir string) (map[string][32]byte, error) {
	sums := map[string][32]byte{}
	err := projectFiles(dir, func(rel, path string) error {
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sums[rel] = sha256.Sum256(src)
		return nil
	})
	return sums, err
}

// copyProject copies the project's files from dir into to.
func copyProject(dir, to string) error {
	return projectFiles(dir, func(rel, path string) error {
		target := filepath.Join(to, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, src, info.Mode().Perm())
	})
}

// commandLine is a command as typed, for titles and results.
func commandLine(parts ...string) string {
	return strings.Join(strings.Fields(strings.Join(parts, " ")), " ")
}
