package scaffold

import (
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"
	"text/template"
)

// Templates live under templates/base with the layout of the generated
// project; templates/database adds and overrides files for a project with
// a SQLite database, and templates/postgres overrides those again for
// PostgreSQL. templates/full adds the files only a project with the admin,
// telemetry and console has, and templates/api the files only a project
// made with --api-only has, and templates/docker the files for building
// and running the app in a container, left out with --no-docker. An API
// project leaves out every file for pages (see uiOnly). A file is rendered with text/template, "__name__" in
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

// Mode is the permissions the file is written with: scripts such as
// bin/start are executable.
func (f File) Mode() os.FileMode {
	if strings.HasPrefix(f.Path, "bin/") {
		return 0o755
	}
	return 0o644
}

// Files renders every template for the project, sorted by path.
func (p *Project) Files() ([]File, error) {
	data := p.data()
	funcs, err := p.funcs()
	if err != nil {
		return nil, err
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
	if !p.NoDocker {
		sets = append(sets, "templates/docker")
	}
	byPath := map[string]File{}
	for _, set := range sets {
		if err := render(set, data, funcs, byPath); err != nil {
			return nil, err
		}
	}
	files := make([]File, 0, len(byPath))
	for _, f := range byPath {
		if p.API && uiOnly(f.Path, p.Name) || p.NoAgents && forAgents(f.Path) {
			continue
		}
		files = append(files, f)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

// DockerFiles renders only the files for containers, sorted by path, for
// 'howdy add docker'.
func (p *Project) DockerFiles() ([]File, error) {
	// The container files name no Howdy package, so they need no source.
	byPath := map[string]File{}
	if err := render("templates/docker", p.data(), nil, byPath); err != nil {
		return nil, err
	}
	files := make([]File, 0, len(byPath))
	for _, f := range byPath {
		files = append(files, f)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

// data is what the templates are rendered with.
func (p *Project) data() map[string]string {
	return map[string]string{
		"Name":  p.Name,
		"Title": p.Title,
		// "sqlite", "postgres" or "" for small differences within a file.
		"Database": string(p.Database),
		// The Howdy release, such as "1.0.0", for its tag "v1.0.0".
		"Release": p.Release,
		// "git" or "hex": where the Howdy packages are downloaded from.
		"Source": string(p.Source),
		// The Git repository the packages come from when it is not
		// Repository, such as a fork; "" otherwise.
		"Repository": p.Repository,
		// "true" unless --minimal: the admin, telemetry and console.
		"Full": flag(!p.Minimal),
		// "true" for --api-only: no Howdy UI, pages or static files.
		"API": flag(p.API),
		// The layout a layout template is rendered as; "default" here.
		"Layout": "default",
		// "true" when the todo list example is there for dev/seed.gleam to
		// seed; see SeedFiles.
		"Todos": flag(p.Database != NoDatabase),
		// "true" unless --no-docker: the Dockerfile and bin/start.
		"Docker": flag(!p.NoDocker),
		// The versions the Dockerfile uses, such as "1.19.2" and "29".
		"Gleam":  p.Gleam,
		"Erlang": p.Erlang,
	}
}

// funcs are the functions the templates call: {{dependency "howdy_ui"}} is
// the gleam.toml line for a Howdy package.
func (p *Project) funcs() (template.FuncMap, error) {
	source, err := ReleaseSource(p.Source, p.Release)
	if err != nil {
		return nil, err
	}
	if source.Kind == Git && p.Repository != "" {
		source.Git = p.Repository
	}
	return template.FuncMap{"dependency": source.Dependency}, nil
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

// seedTemplates are the files 'howdy db seed' runs, by project path.
var seedTemplates = []string{"dev/seed.gleam", "dev/seed/reset.gleam"}

// SeedFiles renders dev/seed.gleam and dev/seed/reset.gleam for the project
// name and database, without the todo list example, for 'howdy db seed' to
// add to a project made before 'howdy new' wrote them.
func SeedFiles(name string, database Database) ([]File, error) {
	data := map[string]string{"Name": name, "Database": string(database), "Todos": ""}
	var files []File
	for _, path := range seedTemplates {
		src, err := templates.ReadFile("templates/database/" + path + ".tmpl")
		if err != nil {
			return nil, err
		}
		tmpl, err := template.New(path).Option("missingkey=error").Parse(string(src))
		if err != nil {
			return nil, err
		}
		var out bytes.Buffer
		if err := tmpl.Execute(&out, data); err != nil {
			return nil, fmt.Errorf("template %s: %w", path, err)
		}
		files = append(files, File{Path: path, Content: out.Bytes()})
	}
	return files, nil
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
// static files, the stylesheet task and the agent skill for pages.
func uiOnly(path, name string) bool {
	for _, prefix := range []string{
		"src/" + name + "/features/home/", "test/" + name + "/features/home/", "src/" + name + "/layouts/", "priv/",
		".claude/skills/howdy-pages/",
	} {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return path == "src/"+name+"/platform/not_found.gleam" || path == "src/tasks/css.gleam"
}

// forAgents reports whether a project file is there for coding agents,
// and so is left out with --no-agents.
func forAgents(path string) bool {
	return path == "AGENTS.md" || path == "CLAUDE.md" || strings.HasPrefix(path, ".claude/skills/")
}

// render every template under root into byPath, replacing earlier entries
// for the same project path.
func render(root string, data map[string]string, funcs template.FuncMap, byPath map[string]File) error {
	return fs.WalkDir(templates, root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		src, err := templates.ReadFile(path)
		if err != nil {
			return err
		}
		tmpl, err := template.New(path).Funcs(funcs).Option("missingkey=error").Parse(string(src))
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
