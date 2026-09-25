package generate

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"howdy/internal/scaffold"
)

// Layouts live in src/<project>/layouts/ as <name>_layout.gleam. Every page
// the generators write renders through one; "default" is the one
// 'howdy new' writes and the one used unless --layout names another.
const DefaultLayout = "default"

// LayoutFile is the path of the layout called name, relative to the
// project directory.
func (p *Project) LayoutFile(name string) string {
	return filepath.ToSlash(filepath.Join("src", p.Name, "layouts", name+"_layout.gleam"))
}

// Layouts lists the names of the project's layouts, sorted.
func (p *Project) Layouts() []string {
	paths, _ := filepath.Glob(filepath.Join(p.Dir, "src", p.Name, "layouts", "*_layout.gleam"))
	var names []string
	for _, path := range paths {
		names = append(names, strings.TrimSuffix(filepath.Base(path), "_layout.gleam"))
	}
	sort.Strings(names)
	return names
}

// ResolveLayout reads a --layout value, "admin" or "admin_layout", and
// checks that the layout exists. An empty value is the default layout,
// which does not have to exist yet: the generators write it into projects
// made before layouts, when a page first needs it.
func (p *Project) ResolveLayout(name string) (string, error) {
	name = strings.TrimSuffix(name, "_layout")
	if name == "" || name == DefaultLayout {
		return DefaultLayout, nil
	}
	if !identifier.MatchString(name) {
		return "", invalidName(name)
	}
	if _, err := os.Stat(filepath.Join(p.Dir, p.LayoutFile(name))); err != nil {
		have := "none yet"
		if names := p.Layouts(); len(names) > 0 {
			have = strings.Join(names, ", ")
		}
		return "", fmt.Errorf("there is no %s layout in src/%s/layouts/ (the project has: %s); create it with 'howdy create layout %s'", name, p.Name, have, name)
	}
	return name, nil
}

// defaultLayout is the default layout as a shared file, which Write only
// creates if the project does not have it.
func (p *Project) defaultLayout() (File, error) {
	content, err := scaffold.Layout(p.Name, DefaultLayout)
	return File{Path: p.LayoutFile(DefaultLayout), Content: content, Shared: true}, err
}

// Layout is a new layout for 'howdy create layout'.
type Layout struct {
	Project *Project
	Name    string
}

// NewLayout validates the name and checks that the layout does not exist.
func NewLayout(p *Project, name string) (*Layout, error) {
	name = strings.TrimSuffix(name, "_layout")
	if !identifier.MatchString(name) {
		return nil, invalidName(name)
	}
	if gleamKeywords[name] {
		return nil, fmt.Errorf("%q is a Gleam keyword; pick another name", name)
	}
	l := &Layout{Project: p, Name: name}
	if _, err := os.Stat(filepath.Join(p.Dir, l.File())); err == nil {
		return nil, fmt.Errorf("the %s layout already exists: %s; pick another name, or edit that layout", name, l.File())
	}
	return l, nil
}

// File is the layout's path, relative to the project directory.
func (l *Layout) File() string { return l.Project.LayoutFile(l.Name) }

// Module is the layout's module name, e.g. "admin_layout".
func (l *Layout) Module() string { return l.Name + "_layout" }

// Write creates the layout, a copy of the one 'howdy new' starts with.
func (l *Layout) Write() error {
	content, err := scaffold.Layout(l.Project.Name, l.Name)
	if err != nil {
		return err
	}
	target := filepath.Join(l.Project.Dir, l.File())
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	return os.WriteFile(target, content, 0o644)
}

// pageStyles adds the styles of the pages 'howdy create pages' writes to
// priv/public/assets/style.css, which every layout links, unless they are
// there already. It reports whether it changed the file.
func (p *Project) pageStyles() (bool, error) {
	path := filepath.Join(p.Dir, "priv", "public", "assets", "style.css")
	current, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	if strings.Contains(string(current), ".page-table") {
		return false, nil
	}
	styles, err := templates.ReadFile("templates/pages.css.tmpl")
	if err != nil {
		return false, err
	}
	text := strings.TrimRight(string(current), "\n")
	if text != "" {
		text += "\n\n"
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	return true, os.WriteFile(path, []byte(text+string(styles)), 0o644)
}
