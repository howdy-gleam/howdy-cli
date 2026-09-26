package scaffold

import (
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"text/template"
)

// Templates live under templates/base with the layout of the generated
// project; templates/database adds and overrides files for a project with
// a SQLite database, and templates/postgres overrides those again for
// PostgreSQL. templates/full adds the files only a project with the admin,
// telemetry and console has, and templates/api the files only a project
// made with --api-only has. An API project leaves out every file for pages
// (see uiOnly). A file is rendered with text/template, "__name__" in
// a path is replaced by the project name, and a trailing ".tmpl" is dropped.
//
//go:embed all:templates
var templates embed.FS

// File is a rendered project file.
type File struct {
	// Path is relative to the project root, with forward slashes.
	Path    string
	Content []byte
}

// Files renders every template for the project, sorted by path.
func (p *Project) Files() ([]File, error) {
	data := map[string]string{
		"Name":  p.Name,
		"Title": p.Title,
		// "sqlite", "postgres" or "" for small differences within a file.
		"Database": string(p.Database),
		// The Howdy release, such as "2.0.0", for its tag "v2.0.0".
		"Release": p.Release,
		// "true" unless --minimal: the admin, telemetry and console.
		"Full": flag(!p.Minimal),
		// "true" for --api-only: no Howdy UI, pages or static files.
		"API": flag(p.API),
		// The layout a layout template is rendered as; "default" here.
		"Layout": "default",
	}
	sets := []string{"templates/base"}
	if p.Database != NoDatabase {
		sets = append(sets, "templates/database")
	}
	if p.Database == Postgres {
		sets = append(sets, "templates/postgres")
	}
	if !p.Minimal {
		sets = append(sets, "templates/full")
	}
	if p.API {
		sets = append(sets, "templates/api")
	}
	byPath := map[string]File{}
	for _, set := range sets {
		if err := render(set, data, byPath); err != nil {
			return nil, err
		}
	}
	files := make([]File, 0, len(byPath))
	for _, f := range byPath {
		if p.API && uiOnly(f.Path, p.Name) {
			continue
		}
		files = append(files, f)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

const layoutTemplate = "templates/base/src/__name__/layouts/default_layout.gleam.tmpl"

// Layout renders a layout named layout ("default", "admin") for the
// project name, from the template 'howdy new' writes the default one with.
func Layout(name, layout string) ([]byte, error) {
	src, err := templates.ReadFile(layoutTemplate)
	if err != nil {
		return nil, err
	}
	tmpl, err := template.New(layoutTemplate).Option("missingkey=error").Parse(string(src))
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	err = tmpl.Execute(&out, map[string]string{"Name": name, "Title": title(name), "Layout": layout})
	return out.Bytes(), err
}

// flag is a template value that is true when it is not empty.
func flag(on bool) string {
	if on {
		return "true"
	}
	return ""
}

// uiOnly reports whether a project file serves pages, and so is left out of
// an API project: the site and its tests, layouts, the HTML 404 page,
// static files and the stylesheet task.
func uiOnly(path, name string) bool {
	for _, prefix := range []string{
		"src/" + name + "/features/home/", "test/" + name + "/features/home/", "src/" + name + "/layouts/", "priv/",
	} {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return path == "src/"+name+"/platform/not_found.gleam" || path == "src/tasks/css.gleam"
}

// render every template under root into byPath, replacing earlier entries
// for the same project path.
func render(root string, data map[string]string, byPath map[string]File) error {
	return fs.WalkDir(templates, root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		src, err := templates.ReadFile(path)
		if err != nil {
			return err
		}
		tmpl, err := template.New(path).Option("missingkey=error").Parse(string(src))
		if err != nil {
			return fmt.Errorf("template %s: %w", path, err)
		}
		var out bytes.Buffer
		if err := tmpl.Execute(&out, data); err != nil {
			return fmt.Errorf("template %s: %w", path, err)
		}
		rel := strings.TrimPrefix(path, root+"/")
		rel = strings.TrimSuffix(rel, ".tmpl")
		rel = strings.ReplaceAll(rel, "__name__", data["Name"])
		byPath[rel] = File{Path: rel, Content: out.Bytes()}
		return nil
	})
}
