// Package scaffold creates a new Howdy project from embedded templates.
package scaffold

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Project describes the project to create.
type Project struct {
	// Name is the Gleam project name, e.g. "my_site".
	Name string
	// Title is a human form of the name for page titles, e.g. "My site".
	Title string
	// Dir is the absolute directory the project is written to.
	Dir string
	// Database is the database the project uses through Gloo, with an
	// example todo list, or NoDatabase for a page-only project.
	Database Database
	// Release is the Howdy release the project depends on, such as
	// "2.1.0": its packages come from the tag "v" + Release.
	Release string
	// Minimal leaves out the development admin, telemetry and the GSH
	// console, which every project gets otherwise.
	Minimal bool
	// API leaves out Howdy UI, Lustre, the pages, layouts and static files:
	// the project is a JSON backend, with a health check in place of the
	// home page.
	API bool
	// NoAgents leaves out the files for coding agents: AGENTS.md,
	// CLAUDE.md and the skills in .claude/skills/.
	NoAgents bool

	arg string
}

// Release is the Howdy release these templates are written and tested
// against, and so the one new projects depend on unless told otherwise.
// Update it with the templates when a Howdy release changes what a project
// needs.
const Release = "2.1.0"

var releasePattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

// ParseRelease reads the value of the --howdy flag: a release such as
// 2.1.0, with or without a leading v.
func ParseRelease(s string) (string, error) {
	release := strings.TrimPrefix(strings.TrimSpace(s), "v")
	if !releasePattern.MatchString(release) {
		return "", fmt.Errorf("%q is not a Howdy release: use a version such as %s", s, Release)
	}
	return release, nil
}

// Database is the kind of database a project is created with.
type Database string

const (
	NoDatabase Database = ""
	SQLite     Database = "sqlite"
	Postgres   Database = "postgres"
)

// ParseDatabase reads the value of the --database flag.
func ParseDatabase(s string) (Database, error) {
	switch db := Database(strings.ToLower(strings.TrimSpace(s))); db {
	case SQLite, Postgres:
		return db, nil
	case "postgresql":
		return Postgres, nil
	}
	return NoDatabase, fmt.Errorf("unknown database %q: use sqlite or postgres", s)
}

// Label is the database's name as written in prose.
func (d Database) Label() string {
	switch d {
	case SQLite:
		return "SQLite"
	case Postgres:
		return "PostgreSQL"
	}
	return "none"
}

var namePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// Names that would shadow a Gleam keyword, a dependency of the generated
// project or a module it imports.
var reserved = map[string]bool{
	"as": true, "assert": true, "auto": true, "case": true, "const": true,
	"delegate": true, "derive": true, "echo": true, "else": true, "fn": true,
	"if": true, "implement": true, "import": true, "let": true, "macro": true,
	"opaque": true, "panic": true, "pub": true, "test": true, "todo": true,
	"type": true, "use": true,
	"gleam": true, "erlang": true, "src": true, "dev": true, "priv": true,
	"howdy": true, "howdy_ui": true, "howdy_dev": true, "howdy_database": true,
	"howdy_admin": true, "howdy_telemetry": true, "howdy_auth": true,
	"howdy_mail": true, "gsh": true, "console": true, "lustre": true,
	"sketch": true, "ewe": true, "logging": true, "dot_env": true,
	"gleeunit": true, "gloo": true, "tasks": true, "home": true, "todos": true,
	"support": true, "pog": true,
}

// New validates the name (or path ending in a name) and resolves the target
// directory, which must not already exist unless it is empty.
func New(arg string) (*Project, error) {
	arg = filepath.Clean(strings.TrimSpace(arg))
	name := filepath.Base(arg)
	if err := ValidateName(name); err != nil {
		return nil, err
	}
	dir, err := filepath.Abs(arg)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	switch {
	case err == nil && len(entries) > 0:
		return nil, fmt.Errorf("%s already exists and is not empty", arg)
	case err != nil && !errors.Is(err, os.ErrNotExist):
		if _, statErr := os.Stat(dir); statErr == nil {
			return nil, fmt.Errorf("%s already exists and is not a directory", arg)
		}
		return nil, err
	}
	return &Project{
		Name:     name,
		Title:    title(name),
		Dir:      dir,
		Database: SQLite,
		Release:  Release,
		arg:      arg,
	}, nil
}

// ValidateName checks that name is usable as a Gleam project name.
func ValidateName(name string) error {
	switch {
	case name == "":
		return errors.New("a project name is required")
	case !namePattern.MatchString(name):
		return fmt.Errorf("%q is not a valid project name: use lowercase letters, digits and underscores, starting with a letter (for example my_site)", name)
	case strings.HasPrefix(name, "gleam_"):
		return fmt.Errorf("%q is not a valid project name: names starting with gleam_ are reserved", name)
	case reserved[name]:
		return fmt.Errorf("%q is reserved by Gleam or Howdy; pick another name", name)
	}
	for _, module := range []string{name, name + "_test", name + "_dev"} {
		if erlangModules[module] {
			return fmt.Errorf("%q is not a valid project name: the project's %s module would replace Erlang's own %s module; pick another name", name, module, module)
		}
	}
	return nil
}

// erlangModules are the modules that ship with Erlang/OTP. A project's
// top-level modules (src/<name>.gleam, test/<name>_test.gleam and so on)
// compile to Erlang modules of the same name, so they must not be one of
// these. Modules under src/<name>/ are prefixed with the project name and
// cannot clash. Regenerate erlang_modules.txt from a new OTP release with:
//
//	ls "$(erl -noshell -eval 'io:put_chars(code:root_dir()), halt().')"/lib/*/ebin/*.beam |
//		xargs -n1 basename | sed 's/\.beam$//' | grep -E '^[a-z][a-z0-9_]*$' | sort -u
//
//go:embed erlang_modules.txt
var erlangModuleList string

var erlangModules = func() map[string]bool {
	modules := map[string]bool{}
	for _, module := range strings.Fields(erlangModuleList) {
		modules[module] = true
	}
	return modules
}()

// Display is the project directory as the user typed it.
func (p *Project) Display() string {
	return p.arg
}

// Write renders every template into the project directory, calling each
// with the relative path of every file as it is created. It returns the
// number of files written.
func (p *Project) Write(each func(path string)) (int, error) {
	files, err := p.Files()
	if err != nil {
		return 0, err
	}
	for _, f := range files {
		target := filepath.Join(p.Dir, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return 0, err
		}
		if err := os.WriteFile(target, f.Content, 0o644); err != nil {
			return 0, err
		}
		if each != nil {
			each(f.Path)
		}
	}
	return len(files), nil
}

// Remove deletes the project directory. Used to clean up after a failure.
func (p *Project) Remove() {
	if p.Dir != "" && p.Dir != "/" {
		_ = os.RemoveAll(p.Dir)
	}
}

func title(name string) string {
	words := strings.Split(name, "_")
	s := strings.Join(words, " ")
	return strings.ToUpper(s[:1]) + s[1:]
}
