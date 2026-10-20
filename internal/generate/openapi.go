package generate

import (
	"bytes"
	"errors"
	"fmt"
	"howdy/internal/scaffold"
	"os"
	"path/filepath"
	"regexp"
)

// OpenAPI adds howdy_openapi to a project: an OpenAPI document of the
// routes written as endpoints, served by the app with a page to read it.
// The existing controllers are left as they are; with the dev admin, the
// admin finds the document by itself and lists the endpoints under API.
type OpenAPI struct {
	Project *Project
	scaffold.Source
	// Admin reports whether the dev entry point mounts the admin.
	Admin bool
}

// NewOpenAPI checks that the project can take howdy_openapi and does not
// have it yet.
func NewOpenAPI(p *Project) (*OpenAPI, error) {
	toml, err := os.ReadFile(filepath.Join(p.Dir, "gleam.toml"))
	if err != nil {
		return nil, err
	}
	if dependency("howdy_openapi").Match(toml) {
		return nil, errors.New("this project already has OpenAPI: howdy_openapi is in gleam.toml")
	}
	source, err := howdySource(toml, "howdy_openapi")
	if err != nil {
		return nil, err
	}
	dev, _ := os.ReadFile(filepath.Join(p.Dir, p.devFile()))
	return &OpenAPI{Project: p, Source: source, Admin: bytes.Contains(dev, []byte("admin.mount("))}, nil
}

// DocsFile is the module that describes and serves the document.
func (o *OpenAPI) DocsFile() string {
	return filepath.ToSlash(filepath.Join("src", o.Project.Name, "platform", "api_docs.gleam"))
}

// Files renders the document's module and, when the project has test
// support, its test.
func (o *OpenAPI) Files() ([]File, error) {
	p := o.Project
	data := struct {
		Name, Title     string
		Database, Admin bool
	}{p.Name, p.Title(), p.Database != "", o.Admin}
	targets := []struct{ template, path string }{
		{"api_docs.gleam.tmpl", o.DocsFile()},
	}
	if _, err := os.Stat(filepath.Join(p.Dir, "test", p.Name, "support.gleam")); err == nil {
		targets = append(targets, struct{ template, path string }{
			"api_docs_test.gleam.tmpl", filepath.Join("test", p.Name, "platform", "api_docs_test.gleam"),
		})
	}
	var files []File
	for _, t := range targets {
		out, err := render(t.template, data)
		if err != nil {
			return nil, err
		}
		files = append(files, File{Path: filepath.ToSlash(t.path), Content: out})
	}
	return files, nil
}

// Write adds howdy_openapi to gleam.toml and creates the files. It returns
// the paths written or changed.
func (o *OpenAPI) Write() ([]string, error) {
	p := o.Project
	files, err := o.Files()
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		if _, err := os.Stat(filepath.Join(p.Dir, f.Path)); err == nil {
			return nil, fmt.Errorf("%s already exists; nothing was written", f.Path)
		}
	}
	line := o.Dependency("howdy_openapi") + "\n"
	if err := insertDependencies(filepath.Join(p.Dir, "gleam.toml"), line); err != nil {
		return nil, err
	}
	changed := []string{"gleam.toml"}
	written, err := writeFiles(p.Dir, files)
	return append(changed, written...), err
}

// The end of the controllers in app(): the static catch-all of a web app,
// or the not-found handler of an API, with the comment 'howdy new' puts
// above it.
var endOfControllers = regexp.MustCompile(`(?m)^([ \t]*)(?:// [^\n]*\n[ \t]*)?\|> howdy\.(?:controller\(static\.serve\(|not_found\()`)

// Register serves the document from the app, after its controllers so it
// documents them all, and ahead of the static catch-all so it is reached.
// The admin needs nothing: it finds the document in the app it is mounted
// on. If src/<project>.gleam does not have the shape 'howdy new' gives it,
// the returned notes say what to add by hand.
func (o *OpenAPI) Register() (notes []string) {
	p := o.Project
	root := filepath.Join("src", p.Name+".gleam")
	err := editFile(filepath.Join(p.Dir, root), func(text string) (string, error) {
		loc := endOfControllers.FindStringSubmatchIndex(text)
		if loc == nil {
			return "", errors.New("no static controller or not_found handler to serve the document ahead of")
		}
		indent := text[loc[2]:loc[3]]
		serve := indent + "// After the controllers it documents: see platform/api_docs.gleam.\n" + indent + "|> api_docs.serve\n"
		text = text[:loc[0]] + serve + text[loc[0]:]
		return addImports(text, "import "+p.Name+"/platform/api_docs"), nil
	})
	if err != nil {
		notes = append(notes, fmt.Sprintf("Serve the document from the app in %s: import %s/platform/api_docs and add |> api_docs.serve after the controllers, ahead of the static controller.", root, p.Name))
	}
	return notes
}
