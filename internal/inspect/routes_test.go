package inspect

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"howdy/internal/scaffold"
)

// project writes a new project with howdy new's templates and returns its
// directory.
func project(t *testing.T, setup func(p *scaffold.Project)) string {
	t.Helper()
	p, err := scaffold.New(filepath.Join(t.TempDir(), "demo_site"))
	if err != nil {
		t.Fatal(err)
	}
	setup(p)
	if _, err := p.Write(nil); err != nil {
		t.Fatal(err)
	}
	return p.Dir
}

func summary(routes []Route) []string {
	var lines []string
	for _, r := range routes {
		line := r.Method + " " + r.Path + " " + r.Controller
		if r.Guarded {
			line += " guarded"
		}
		if r.Dev {
			line += " dev"
		}
		lines = append(lines, line)
	}
	return lines
}

func TestRoutesOfNewProject(t *testing.T) {
	dir := project(t, func(p *scaffold.Project) {})
	routes, err := Routes(dir, "demo_site")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"GET /api/todos todos.controller",
		"GET /api/todos/:id todos.controller",
		"POST /api/todos todos.controller",
		"PATCH /api/todos/:id todos.controller",
		"DELETE /api/todos/:id todos.controller",
		"GET / home.controller",
		"GET /live/todos home.controller",
		"* / static.serve",
		"* /assets/ui.css home.stylesheet dev",
		"* /_howdy admin.mount dev",
	}
	if got := summary(routes); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("routes:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for _, r := range routes {
		src, err := os.ReadFile(filepath.Join(dir, r.File))
		if err != nil {
			t.Fatalf("%s: %v", r.File, err)
		}
		lines := strings.Split(string(src), "\n")
		if r.Line < 1 || r.Line > len(lines) {
			t.Fatalf("%s:%d is past the end of the file", r.File, r.Line)
		}
		if r.Method != "*" && !strings.Contains(lines[r.Line-1], "."+strings.ToLower(r.Method)+"(") {
			t.Errorf("%s:%d should declare %s %s: %q", r.File, r.Line, r.Method, r.Path, lines[r.Line-1])
		}
	}
}

func TestRoutesOfAPIProject(t *testing.T) {
	dir := project(t, func(p *scaffold.Project) {
		p.API, p.Minimal, p.Database = true, true, scaffold.NoDatabase
	})
	routes, err := Routes(dir, "demo_site")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(summary(routes), "\n"); got != "GET /health health.controller" {
		t.Errorf("routes:\n%s", got)
	}
}

func TestRoutesOfGuardedMountedAndDocumentedControllers(t *testing.T) {
	dir := project(t, func(p *scaffold.Project) {
		p.API, p.Minimal, p.Database = true, true, scaffold.NoDatabase
	})
	write := func(path, text string) {
		t.Helper()
		full := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("src/demo_site.gleam", `import howdy
import howdy/auth/routes
import demo_site/features/notes/notes_controller
import demo_site/platform/api_docs

pub fn app(services: Services) -> howdy.App {
  howdy.new()
  |> howdy.controller(routes.api(services.identity, at: "/api/auth"))
  |> howdy.controller(notes_controller.controller(services.identity))
  |> api_docs.serve
}
`)
	write("src/demo_site/features/notes/notes_controller.gleam", `import howdy/controller

pub fn controller(identity: Auth) {
  controller.guarded("/notes/", auth.required(identity))
  |> controller.get("/", fn(ctx) { todo })
  |> endpoint.post("/:id", create())
  |> controller.route(http.Put, "/:id/*rest", fn(ctx) { todo })
  |> controller.build
}

fn helper() {
  controller.get("/not/a/route", fn(ctx) { todo })
}
`)
	write("src/demo_site/platform/api_docs.gleam", `pub const document_path = "/spec.json"
`)
	routes, err := Routes(dir, "demo_site")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"* /api/auth routes.api",
		"GET /notes notes_controller.controller guarded",
		"POST /notes/:id notes_controller.controller guarded",
		"PUT /notes/:id/*rest notes_controller.controller guarded",
		"GET /spec.json api_docs.serve",
		"GET /docs api_docs.serve",
	}
	if got := summary(routes); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("routes:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if routes[0].Mount != "Howdy Auth's JSON API" || routes[0].Line != 8 {
		t.Errorf("auth mount = %+v", routes[0])
	}
}

func TestJoin(t *testing.T) {
	for _, c := range [][3]string{
		{"", "/", "/"},
		{"api/todos", "/", "/api/todos"},
		{"/blog/", "/:id/edit", "/blog/:id/edit"},
		{"", "/live/todos", "/live/todos"},
	} {
		if got := join(c[0], c[1]); got != c[2] {
			t.Errorf("join(%q, %q) = %q, want %q", c[0], c[1], got, c[2])
		}
	}
}
