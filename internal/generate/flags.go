package generate

import (
	"bytes"
	"errors"
	"fmt"
	"howdy/internal/scaffold"
	"os"
	"path/filepath"
	"strings"
)

// Flags adds howdy_flags to a project: feature flags defined in code, with
// their settings kept in the app's database, so they can be turned off at
// once, allowed or blocked for users, organizations and groups, and rolled
// out gradually while the app runs. The running flags are `features` in the
// app's services, managed in the dev admin and with `gleam run -m
// tasks/flags`.
type Flags struct {
	Project *Project
	scaffold.Source
	Admin bool
}

// NewFlags checks that the project can take flags and does not have them
// yet.
func NewFlags(p *Project) (*Flags, error) {
	toml, err := os.ReadFile(filepath.Join(p.Dir, "gleam.toml"))
	if err != nil {
		return nil, err
	}
	switch {
	case dependency("howdy_flags").Match(toml):
		return nil, errors.New("this project already has feature flags: howdy_flags is in gleam.toml")
	case !dependency("howdy_database").Match(toml):
		return nil, errors.New("feature flags keep their settings in the app's database through howdy_database, which this project does not have; create a project with a database ('howdy new' without --no-database) to use them")
	}
	source, err := howdySource(toml, "howdy_flags")
	if err != nil {
		return nil, err
	}
	root := readText(filepath.Join(p.Dir, "src", p.Name+".gleam"))
	if !servicesType.MatchString(root) || !servicesMade.MatchString(root) {
		return nil, fmt.Errorf("src/%s.gleam was made before 'howdy new' built the app from a services record, which the flags are added to; see the flags docs to add them by hand", p.Name)
	}
	f := &Flags{Project: p, Source: source}
	f.Admin = bytes.Contains([]byte(readText(filepath.Join(p.Dir, p.devFile()))), []byte("admin.mount("))
	return f, nil
}

// FlagsFile is the module that defines the flags and starts them.
func (f *Flags) FlagsFile() string {
	return filepath.ToSlash(filepath.Join("src", f.Project.Name, "feature_flags.gleam"))
}

// TaskFile is the task module that manages them from the command line.
func (f *Flags) TaskFile() string {
	return filepath.ToSlash(filepath.Join("src", "tasks", "flags.gleam"))
}

// Files renders the flags module, the command-line task and, when the
// project has test support, its test.
func (f *Flags) Files() ([]File, error) {
	p := f.Project
	targets := []struct{ template, path string }{
		{"feature_flags.gleam.tmpl", f.FlagsFile()},
		{"flags_task.gleam.tmpl", f.TaskFile()},
	}
	if _, err := os.Stat(filepath.Join(p.Dir, "test", p.Name, "support.gleam")); err == nil {
		targets = append(targets, struct{ template, path string }{
			"feature_flags_test.gleam.tmpl", filepath.Join("test", p.Name, "feature_flags_test.gleam"),
		})
	}
	var files []File
	for _, t := range targets {
		out, err := render(t.template, p)
		if err != nil {
			return nil, err
		}
		files = append(files, File{Path: filepath.ToSlash(t.path), Content: out})
	}
	return files, nil
}

// Write adds howdy_flags to gleam.toml and creates the files. It returns
// the paths written or changed.
func (f *Flags) Write() ([]string, error) {
	p := f.Project
	files, err := f.Files()
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		if _, err := os.Stat(filepath.Join(p.Dir, file.Path)); err == nil {
			return nil, fmt.Errorf("%s already exists; nothing was written", file.Path)
		}
	}
	line := f.Dependency("howdy_flags") + "\n"
	if err := insertDependencies(filepath.Join(p.Dir, "gleam.toml"), line); err != nil {
		return nil, err
	}
	changed := []string{"gleam.toml"}
	written, err := writeFiles(p.Dir, files)
	return append(changed, written...), err
}

// Register runs the flags' migrations after the app's, starts them as
// `features` in the app's services wherever those are built, and with the
// dev admin, manages them there. For each file without the shape 'howdy
// new' gives it, the returned notes say what to add by hand.
func (f *Flags) Register() (notes []string) {
	p := f.Project
	module := "import " + p.Name + "/feature_flags"
	start := func(text string) (string, error) {
		return addToServices(text, "features", "feature_flags.start(db)")
	}

	root := filepath.Join("src", p.Name+".gleam")
	err := editFile(filepath.Join(p.Dir, root), func(text string) (string, error) {
		text, err := addServicesField(text, "features: flags.Flags")
		if err != nil {
			return "", err
		}
		if text, err = start(text); err != nil {
			return "", err
		}
		return addImports(text, "import howdy/flags", module), nil
	})
	if err != nil {
		notes = append(notes, fmt.Sprintf("Add the flags to the app in %s: features: flags.Flags in Services, built in main with feature_flags.start(db) after migrating (import howdy/flags and %s/feature_flags).", root, p.Name))
	}

	database := filepath.Join("src", p.Name, "platform", "database.gleam")
	err = editFile(filepath.Join(p.Dir, database), func(text string) (string, error) {
		return addPackageMigration(text, "feature_flags.migrate(db)", module)
	})
	if err != nil {
		notes = append(notes, fmt.Sprintf("Run the flags' migrations in %s: call feature_flags.migrate(db) after the app's own, before the app starts.", database))
	}

	dev := p.devFile()
	err = editFile(filepath.Join(p.Dir, dev), func(text string) (string, error) {
		text, err := start(text)
		if err != nil {
			return "", err
		}
		if f.Admin {
			const anchor = "|> admin.telemetry(recorder)"
			if !strings.Contains(text, anchor) {
				return "", errors.New("no admin.telemetry to follow")
			}
			text = strings.Replace(text, anchor, anchor+"\n|> admin.flags(features)", 1)
		}
		return addImports(text, module), nil
	})
	if err != nil {
		admin := ""
		if f.Admin {
			admin = ", and add |> admin.flags(features) to the admin"
		}
		notes = append(notes, fmt.Sprintf("Give the dev server the flags in %s: pass features: feature_flags.start(db) to Services%s.", dev, admin))
	}

	support := filepath.Join("test", p.Name, "support.gleam")
	if _, err := os.Stat(filepath.Join(p.Dir, support)); err == nil {
		err := editFile(filepath.Join(p.Dir, support), func(text string) (string, error) {
			text, err := start(text)
			if err != nil {
				return "", err
			}
			return addImports(text, module), nil
		})
		if err != nil {
			notes = append(notes, fmt.Sprintf("Give the tests the flags in %s: pass features: feature_flags.start(db) to Services.", support))
		}
	}
	return notes
}
