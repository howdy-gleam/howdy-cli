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
// telemetry and console has. A file is rendered with text/template, "__name__" in
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
		"Full": full(p.Minimal),
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
	byPath := map[string]File{}
	for _, set := range sets {
		if err := render(set, data, byPath); err != nil {
			return nil, err
		}
	}
	files := make([]File, 0, len(byPath))
	for _, f := range byPath {
		files = append(files, f)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

func full(minimal bool) string {
	if minimal {
		return ""
	}
	return "true"
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
