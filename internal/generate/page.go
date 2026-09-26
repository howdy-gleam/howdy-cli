package generate

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
)

// Page is a single page added to a controller, which is created first if
// the project does not have it yet.
type Page struct {
	Project *Project
	// Controller names the feature folder and the controller module, e.g.
	// "blog" for src/<project>/features/blog/blog_controller.gleam.
	Controller string
	// Name names the page module without its suffix, e.g. "about" for
	// blog/pages/about_page.gleam.
	Name string
	// Live adds a Lustre server component and the route of its socket.
	Live bool
	// Prefix is the path the controller is mounted at, without slashes:
	// the controller name for a new one, whatever an existing one uses.
	Prefix string
	// Exists reports whether the controller is already in the project.
	Exists bool
	// Layout names the layout the page renders through, such as "default".
	Layout string
	// Auth reports whether the controller guards its routes with Howdy
	// Auth: a new one made with --add-auth, or an existing guarded one.
	Auth bool
}

// NewPage validates the names and checks that the controller does not
// already have the page. layout is a --layout value, empty for the default,
// and addAuth is --add-auth, which guards a new controller's routes.
func NewPage(p *Project, controller, name string, live bool, layout string, addAuth bool) (*Page, error) {
	if err := p.NeedsPages("pages"); err != nil {
		return nil, err
	}
	name = strings.TrimSuffix(name, "_page")
	for _, n := range []string{controller, name} {
		if !identifier.MatchString(n) {
			return nil, invalidName(n)
		}
	}
	if gleamKeywords[controller] {
		return nil, fmt.Errorf("%q is a Gleam keyword and cannot name a folder of modules; pick another name", controller)
	}
	layout, err := p.ResolveLayout(layout)
	if err != nil {
		return nil, err
	}
	pg := &Page{Project: p, Controller: controller, Name: name, Live: live, Prefix: controller, Layout: layout}
	if _, err := os.Stat(filepath.Join(p.Dir, pg.File())); err == nil {
		return nil, fmt.Errorf("the page %q already exists in the %s controller: %s; pick another name, or edit that page", name, controller, pg.File())
	}
	src, err := os.ReadFile(filepath.Join(p.Dir, pg.ControllerFile()))
	switch {
	case err == nil:
		pg.Exists = true
		if m := controllerPrefix.FindSubmatch(src); m != nil {
			pg.Prefix = strings.Trim(string(m[1]), "/")
		}
		for _, path := range pg.routes() {
			if hasRoute(src, path) {
				hint := ""
				if name == "index" {
					hint = ", which is where an index page goes"
				}
				return nil, fmt.Errorf("the %s controller already has a page at %s%s (%s); pick another name", controller, pg.url(path), hint, pg.ControllerFile())
			}
		}
	case !errors.Is(err, os.ErrNotExist):
		return nil, err
	}
	if pg.Auth, err = p.guarded(pg.ControllerFile(), src, pg.Exists, addAuth); err != nil {
		return nil, err
	}
	return pg, nil
}

// paramRoute is the start of a line routing a path with a parameter, such
// as `|> controller.get("/:id", ...`.
var paramRoute = regexp.MustCompile(`(?m)^[ \t]*\|> controller\.\w+\(\s*(?:http\.\w+,\s*)?"[^"]*:`)

// buildCall is the line that finishes a guarded controller's routes.
var buildCall = regexp.MustCompile(`(?m)^[ \t]*\|> controller\.build\b`)

var controllerPrefix = regexp.MustCompile(`controller\.new\(\s*"([^"]*)"`)

// hasRoute reports whether a controller's source declares a route at path,
// with any method.
func hasRoute(src []byte, path string) bool {
	return regexp.MustCompile(`controller\.\w+\(\s*(?:http\.\w+,\s*)?"` + regexp.QuoteMeta(path) + `"`).Match(src)
}

// LayoutModule is the module name of the page's layout, e.g. "default_layout".
func (pg *Page) LayoutModule() string { return pg.Layout + "_layout" }

// Module is the page's module name, e.g. "about_page".
func (pg *Page) Module() string { return pg.Name + "_page" }

// ControllerModule is the controller's module name, e.g. "blog_controller".
func (pg *Page) ControllerModule() string { return pg.Controller + "_controller" }

// File and ControllerFile are relative to the project directory.
func (pg *Page) File() string {
	return filepath.ToSlash(filepath.Join(pg.Project.FeatureDir(pg.Controller), "pages", pg.Module()+".gleam"))
}

func (pg *Page) ControllerFile() string {
	return filepath.ToSlash(filepath.Join(pg.Project.FeatureDir(pg.Controller), pg.ControllerModule()+".gleam"))
}

// Title names the page for people, e.g. "Contact us". An index page is
// named after its controller, e.g. "Blog".
func (pg *Page) Title() string {
	if pg.Name == "index" {
		return pg.ControllerTitle()
	}
	return humanize(pg.Name, true)
}

// ControllerTitle names the controller for people, e.g. "Blog".
func (pg *Page) ControllerTitle() string { return humanize(pg.Controller, true) }

// Route is the page's path within the controller: "/" for an index page,
// otherwise "/<name>". SocketRoute is the path of a live page's socket.
func (pg *Page) Route() string {
	if pg.Name == "index" {
		return "/"
	}
	return "/" + pg.Name
}

func (pg *Page) SocketRoute() string {
	if pg.Name == "index" {
		return "/live"
	}
	return "/" + pg.Name + "/live"
}

func (pg *Page) routes() []string {
	if pg.Live {
		return []string{pg.Route(), pg.SocketRoute()}
	}
	return []string{pg.Route()}
}

// URL and SocketURL are the full paths the browser uses.
func (pg *Page) URL() string       { return pg.url(pg.Route()) }
func (pg *Page) SocketURL() string { return pg.url(pg.SocketRoute()) }

func (pg *Page) url(route string) string {
	if pg.Prefix == "" {
		return route
	}
	return strings.TrimSuffix("/"+pg.Prefix+route, "/")
}

// HasSupport reports whether the project has test/<project>/support.gleam,
// which 'howdy new' writes into every project, and which builds the app
// with its services for the tests.
func (pg *Page) HasSupport() bool {
	_, err := os.Stat(filepath.Join(pg.Project.Dir, "test", pg.Project.Name, "support.gleam"))
	return err == nil
}

// HasDatabase reports whether the test uses a database from test/<project>/support.gleam.
func (pg *Page) HasDatabase() bool { return pg.Project.Database != "" }

// Files renders the page, the controller if it is new, and a test when the
// project has the tests `howdy new` gives it.
func (pg *Page) Files() ([]File, error) {
	p := pg.Project
	targets := [][2]string{}
	page := "page.gleam.tmpl"
	if pg.Live {
		page = "live_page.gleam.tmpl"
	}
	targets = append(targets, [2]string{page, pg.File()})
	if !pg.Exists {
		targets = append(targets, [2]string{"page_controller.gleam.tmpl", pg.ControllerFile()})
	}
	support := filepath.Join(p.Dir, "test", p.Name, "support.gleam")
	if pg.HasDatabase() {
		if _, err := os.Stat(support); err == nil {
			targets = append(targets, [2]string{"page_test.gleam.tmpl", pg.testFile()})
		}
	} else if _, err := os.Stat(filepath.Join(p.Dir, "test")); err == nil {
		targets = append(targets, [2]string{"page_test.gleam.tmpl", pg.testFile()})
	}
	var files []File
	for _, t := range targets {
		tmpl, err := template.New(t[0]).Funcs(funcs).Option("missingkey=error").ParseFS(templates, "templates/"+t[0])
		if err != nil {
			return nil, err
		}
		var out bytes.Buffer
		if err := tmpl.Execute(&out, pg); err != nil {
			return nil, fmt.Errorf("template %s: %w", t[0], err)
		}
		files = append(files, File{Path: t[1], Content: out.Bytes()})
	}
	if pg.Layout == DefaultLayout {
		layout, err := p.defaultLayout()
		if err != nil {
			return nil, err
		}
		files = append(files, layout)
	}
	return files, nil
}

func (pg *Page) testFile() string {
	return filepath.ToSlash(filepath.Join(pg.Project.FeatureTestDir(pg.Controller), "pages", pg.Module()+"_test.gleam"))
}

// Write creates the files, refusing to replace any that exist, except that
// shared files already in the project are kept, and for a guarded
// controller adds sign_in to the test support. It returns the paths written.
func (pg *Page) Write() ([]string, error) {
	files, err := pg.Files()
	if err != nil {
		return nil, err
	}
	written, err := writeFiles(pg.Project.Dir, files)
	if err != nil || !pg.Auth {
		return written, err
	}
	return pg.Project.addSignIn(written)
}

// writeFiles creates files under dir, refusing to replace any that exist,
// except that shared files already there are kept. It returns the paths
// written.
func writeFiles(dir string, files []File) ([]string, error) {
	exists := map[string]bool{}
	for _, f := range files {
		if _, err := os.Stat(filepath.Join(dir, f.Path)); err == nil {
			if !f.Shared {
				return nil, fmt.Errorf("%s already exists; nothing was written", f.Path)
			}
			exists[f.Path] = true
		}
	}
	var written []string
	for _, f := range files {
		if exists[f.Path] {
			continue
		}
		target := filepath.Join(dir, f.Path)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return written, err
		}
		if err := os.WriteFile(target, f.Content, 0o644); err != nil {
			return written, err
		}
		written = append(written, f.Path)
	}
	return written, nil
}

// Register adds the page's routes to its controller and, for a new
// controller, the controller to the app in src/<project>.gleam. For each
// file that does not have the shape the edit expects, the returned notes
// say what to add by hand.
func (pg *Page) Register() (notes []string) {
	p := pg.Project
	var routes strings.Builder
	fmt.Fprintf(&routes, "|> controller.get(%q, %s.show)\n", pg.Route(), pg.Module())
	if pg.Live {
		fmt.Fprintf(&routes, "|> controller.get(%q, %s.socket)\n", pg.SocketRoute(), pg.Module())
	}
	importLine := fmt.Sprintf("import %s/pages/%s", p.FeatureImport(pg.Controller), pg.Module())
	if err := appendToFunction(filepath.Join(p.Dir, pg.ControllerFile()), "controller", importLine, routes.String()); err != nil {
		notes = append(notes, fmt.Sprintf("Add the page to %s: %s, and add %s to the routes in its controller function.",
			pg.ControllerFile(), importLine, strings.TrimSpace(strings.ReplaceAll(routes.String(), "\n", " "))))
	}
	if pg.Exists {
		return notes
	}
	root := filepath.Join("src", p.Name+".gleam")
	controller := pg.ControllerModule() + ".controller()"
	if pg.Auth {
		controller = pg.ControllerModule() + ".controller(services.identity)"
	}
	err := edit(filepath.Join(p.Dir, root),
		fmt.Sprintf("import %s/%s", p.FeatureImport(pg.Controller), pg.ControllerModule()),
		// Before the first controller, so it stays ahead of the static
		// catch-all.
		regexp.MustCompile(`(?m)^(\s*)howdy\.new\(\)\n`),
		"${0}${1}|> howdy.controller("+controller+")\n",
	)
	if err != nil {
		notes = append(notes, fmt.Sprintf("Register the controller in %s: import %s/%s and add |> howdy.controller(%s) before the static controller.",
			root, p.FeatureImport(pg.Controller), pg.ControllerModule(), controller))
	}
	return notes
}

// appendToFunction adds lines to the end of the body of `pub fn name(...)`,
// where a controller function ends its pipeline of routes, or before its
// first route with a path parameter, or before `|> controller.build`,
// which ends a guarded controller's routes so any after it would be
// unguarded. The import goes after the file's last import.
func appendToFunction(path, name, importLine, lines string) error {
	src, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	text := string(src)
	start := strings.Index(text, "pub fn "+name+"(")
	imports := lastImport.FindAllStringIndex(text, -1)
	if start < 0 || len(imports) == 0 {
		return fmt.Errorf("%s has no function %s", path, name)
	}
	params := start + len("pub fn "+name)
	afterParams := matching(text, params, '(', ')')
	if afterParams < 0 {
		return fmt.Errorf("%s: unbalanced parentheses", path)
	}
	open := strings.IndexByte(text[afterParams:], '{')
	if open < 0 {
		return fmt.Errorf("%s: %s has no body", path, name)
	}
	end := matching(text, afterParams+open, '{', '}')
	if end < 0 {
		return fmt.Errorf("%s: unbalanced braces", path)
	}
	closing := end - 1
	// Routes match in order, so a literal path such as "/stats" must come
	// before a parameter such as "/:id" that would take it.
	if loc := paramRoute.FindStringIndex(text[afterParams+open : closing]); loc != nil {
		at := afterParams + open + loc[0]
		text = text[:at] + lines + text[at:]
	} else if loc := buildCall.FindStringIndex(text[afterParams+open : closing]); loc != nil {
		at := afterParams + open + loc[0]
		text = text[:at] + lines + text[at:]
	} else {
		body := strings.TrimRight(text[:closing], " \t\n")
		text = body + "\n" + lines + text[closing:]
	}
	if !strings.Contains(text, importLine+"\n") {
		at := imports[len(imports)-1][1]
		text = text[:at] + importLine + "\n" + text[at:]
	}
	return os.WriteFile(path, []byte(text), 0o644)
}

// matching returns the index just past the bracket that closes the one at
// text[at], skipping string literals and comments, or -1.
func matching(text string, at int, open, close byte) int {
	depth := 0
	for i := at; i < len(text); i++ {
		switch c := text[i]; {
		case c == '"':
			for i++; i < len(text) && text[i] != '"'; i++ {
				if text[i] == '\\' {
					i++
				}
			}
		case c == '/' && i+1 < len(text) && text[i+1] == '/':
			for i < len(text) && text[i] != '\n' {
				i++
			}
		case c == open:
			depth++
		case c == close:
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return -1
}
