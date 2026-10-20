package generate

import (
	"errors"
	"fmt"
	"howdy/internal/scaffold"
	"path/filepath"
	"strings"
)

// I18n adds howdy_i18n to a project: message catalogues in locales/, a
// module generated from them with a function for each message, and the
// locale each request is answered in, stamped on every response. Under
// `gleam dev` the module is regenerated whenever a catalogue changes.
type I18n struct {
	Project *Project
	scaffold.Source
}

// NewI18n checks that the project can take translations and does not have
// them yet.
func NewI18n(p *Project) (*I18n, error) {
	source, _, err := installable(p, "howdy_i18n", "translations", false)
	if err != nil {
		return nil, err
	}
	return &I18n{Project: p, Source: source}, nil
}

// LocalesFile is the module that chooses each request's locale.
func (i *I18n) LocalesFile() string {
	return filepath.ToSlash(filepath.Join("src", i.Project.Name, "platform", "locales.gleam"))
}

// MessagesFile is the module generated from the catalogues.
func (i *I18n) MessagesFile() string {
	return filepath.ToSlash(filepath.Join("src", i.Project.Name, "messages.gleam"))
}

// Files renders the source catalogue, the module generated from it, the
// locales module and, when the project has tests, a test of the messages.
func (i *I18n) Files() ([]File, error) {
	p := i.Project
	targets := []struct{ template, path string }{
		{"locale_en.json.tmpl", filepath.Join("locales", "en.json")},
		{"messages.gleam.tmpl", i.MessagesFile()},
		{"locales.gleam.tmpl", i.LocalesFile()},
	}
	if hasSupport(p) {
		targets = append(targets, struct{ template, path string }{
			"messages_test.gleam.tmpl", filepath.Join("test", p.Name, "messages_test.gleam"),
		})
	}
	return renderAll(p, targets)
}

// Write adds howdy_i18n to gleam.toml and creates the files. It returns
// the paths written or changed.
func (i *I18n) Write() ([]string, error) {
	files, err := i.Files()
	if err != nil {
		return nil, err
	}
	line := i.Dependency("howdy_i18n") + "\n"
	// The generated module imports gleam/time/calendar once a message
	// writes a date or a time.
	if !dependency("gleam_time").MatchString(readText(filepath.Join(i.Project.Dir, "gleam.toml"))) {
		line += "gleam_time = \">= 1.8.0 and < 2.0.0\"\n"
	}
	return installFiles(i.Project, line, files)
}

// Register stamps every response with the request's locale, and makes the
// dev server regenerate the messages when a catalogue changes. For each
// file without the shape 'howdy new' gives it, the returned notes say what
// to add by hand.
func (i *I18n) Register() (notes []string) {
	p := i.Project
	module := "import " + p.Name + "/platform/locales"

	root := filepath.Join("src", p.Name+".gleam")
	err := editFile(filepath.Join(p.Dir, root), func(text string) (string, error) {
		if !howdyNew.MatchString(text) {
			return "", errors.New("no howdy.new() in app")
		}
		text = howdyNew.ReplaceAllString(text, "${0}${1}|> howdy.middleware(locale.middleware(locales.negotiator()))\n")
		return addImports(text, "import howdy/locale", module), nil
	})
	if err != nil {
		notes = append(notes, fmt.Sprintf("Stamp responses with their locale in %s: add |> howdy.middleware(locale.middleware(locales.negotiator())) after howdy.new() in app (import howdy/locale and %s/platform/locales).", root, p.Name))
	}

	dev := p.devFile()
	err = editFile(filepath.Join(p.Dir, dev), func(text string) (string, error) {
		const run = "|> dev.run"
		if !strings.Contains(text, run) {
			return "", errors.New("no dev.run")
		}
		text = strings.Replace(text, run, "|> dev.watch(\"locales\")\n"+
			"// Regenerate src/"+p.Name+"/messages.gleam when a catalogue changes.\n"+
			"|> dev.before_build(fn(_changed) { cli.execute([\"gen\"]) })\n"+run, 1)
		return addImports(text, "import howdy/i18n/cli"), nil
	})
	if err != nil {
		notes = append(notes, fmt.Sprintf("Regenerate the messages under gleam dev in %s: add |> dev.watch(\"locales\") |> dev.before_build(fn(_changed) { cli.execute([\"gen\"]) }) before dev.run (import howdy/i18n/cli).", dev))
	}
	return notes
}
