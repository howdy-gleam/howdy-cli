package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateName(t *testing.T) {
	for _, ok := range []string{"my_site", "a", "blog2", "howdy_app"} {
		if err := ValidateName(ok); err != nil {
			t.Errorf("%q should be valid: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "MySite", "1site", "my-site", "gleam_thing", "test", "howdy", "type",
		// Erlang/OTP modules, and a project whose test module would be one.
		"pg", "lists", "json", "auth", "timer", "ct_property"} {
		if err := ValidateName(bad); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

func TestTitle(t *testing.T) {
	if got := title("my_cool_site"); got != "My cool site" {
		t.Errorf("title = %q", got)
	}
}

func TestNewRejectsNonEmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	full := filepath.Join(dir, "taken")
	os.MkdirAll(full, 0o755)
	os.WriteFile(filepath.Join(full, "x"), nil, 0o644)
	if _, err := New(full); err == nil {
		t.Fatal("expected an error for a non-empty directory")
	}
	if _, err := New(filepath.Join(dir, "fresh")); err != nil {
		t.Fatalf("fresh directory: %v", err)
	}
}

func TestWriteRendersProject(t *testing.T) {
	dir := t.TempDir()
	p, err := New(filepath.Join(dir, "demo_site"))
	if err != nil {
		t.Fatal(err)
	}
	p.Database = NoDatabase

	var seen []string
	n, err := p.Write(func(path string) { seen = append(seen, path) })
	if err != nil {
		t.Fatal(err)
	}
	if n != len(seen) || n == 0 {
		t.Fatalf("wrote %d files, reported %d", n, len(seen))
	}

	want := []string{
		"gleam.toml", "README.md", ".gitignore", ".env.example",
		"src/demo_site.gleam", "src/demo_site/features/home/home_controller.gleam", "src/demo_site/features/home/pages/index.gleam", "src/demo_site/layouts/default_layout.gleam",
		"src/tasks/css.gleam", "dev/demo_site_dev.gleam",
		"test/demo_site_test.gleam", "test/demo_site/features/home/home_controller_test.gleam",
		"priv/public/assets/style.css", "priv/public/assets/ui.css",
		"src/demo_site/features/home/pages/not_found_page.gleam", "src/demo_site/platform/not_found.gleam",
		"test/demo_site/features/home/not_found_test.gleam",
	}
	for _, w := range want {
		if _, err := os.Stat(filepath.Join(p.Dir, w)); err != nil {
			t.Errorf("missing %s", w)
		}
	}

	toml, _ := os.ReadFile(filepath.Join(p.Dir, "gleam.toml"))
	for _, line := range []string{
		`name = "demo_site"`,
		`howdy = { git = "https://github.com/mikeyjones/howdy.git", ref = "v` + Release + `" }`,
		`howdy_ui = { git = "https://github.com/mikeyjones/howdy.git", ref = "v` + Release + `", path = "ui" }`,
		`howdy_dev = { git = "https://github.com/mikeyjones/howdy.git", ref = "v` + Release + `", path = "howdy_dev" }`,
	} {
		if !strings.Contains(string(toml), line) {
			t.Errorf("gleam.toml missing %q", line)
		}
	}
	if strings.Contains(string(toml), "gloo") {
		t.Error("page-only project should not depend on gloo")
	}
	home, _ := os.ReadFile(filepath.Join(p.Dir, "src/demo_site/features/home/pages/index.gleam"))
	if !strings.Contains(string(home), `ui.h1("Howdy, Demo site!")`) {
		t.Error("home page does not use the project title")
	}
	if strings.Contains(string(home), "{{") {
		t.Error("unrendered template markers in home page")
	}
	if _, err := os.Stat(filepath.Join(p.Dir, "src/demo_site/features/todos/todos.gleam")); err == nil {
		t.Error("page-only project should not contain the todos slice")
	}
}

func TestDatabaseOverlay(t *testing.T) {
	p, err := New(filepath.Join(t.TempDir(), "demo_site"))
	if err != nil {
		t.Fatal(err)
	}
	if p.Database != SQLite {
		t.Fatal("database should be SQLite by default")
	}
	files, err := p.Files()
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]string{}
	for _, f := range files {
		byPath[f.Path] = string(f.Content)
		if strings.Contains(string(f.Content), "{{") {
			t.Errorf("unrendered template markers in %s", f.Path)
		}
	}
	for _, want := range []string{
		"src/demo_site/features/todos/todo_item.gleam", "src/demo_site/features/todos/todos.gleam",
		"src/demo_site/features/todos/todo_controller.gleam", "src/demo_site/features/home/components/todo_list_live_component.gleam",
		"src/demo_site/migrations/create_todos.gleam", "src/demo_site/platform/database.gleam",
		"src/tasks/migrate.gleam", "test/demo_site/support.gleam", "test/demo_site_test_ffi.erl",
		"priv/public/assets/ui.css", "src/tasks/css.gleam",
	} {
		if _, ok := byPath[want]; !ok {
			t.Errorf("missing %s", want)
		}
	}
	if !strings.Contains(byPath["gleam.toml"], "gloo") {
		t.Error("gleam.toml should depend on gloo")
	}
	if !strings.Contains(byPath["src/demo_site.gleam"], "database.migrate") {
		t.Error("the database composition root should override the base one")
	}
	if !strings.Contains(byPath["test/demo_site_test_ffi.erl"], "-module(demo_site_test_ffi)") {
		t.Error("erlang ffi module should be named after the project")
	}
	if _, ok := byPath["docker-compose.yml"]; ok {
		t.Error("a SQLite project should not get a docker-compose.yml")
	}
	for path, content := range byPath {
		if strings.Contains(strings.ToLower(content), "postgres") {
			t.Errorf("%s mentions postgres in a SQLite project", path)
		}
	}
}

func TestParseDatabase(t *testing.T) {
	for in, want := range map[string]Database{
		"sqlite": SQLite, "postgres": Postgres, "Postgres": Postgres, "postgresql": Postgres,
	} {
		if got, err := ParseDatabase(in); err != nil || got != want {
			t.Errorf("ParseDatabase(%q) = %q, %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "mysql", "none"} {
		if _, err := ParseDatabase(bad); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

func TestPostgresOverlay(t *testing.T) {
	p, err := New(filepath.Join(t.TempDir(), "demo_site"))
	if err != nil {
		t.Fatal(err)
	}
	p.Database = Postgres
	files, err := p.Files()
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]string{}
	for _, f := range files {
		byPath[f.Path] = string(f.Content)
		if strings.Contains(string(f.Content), "{{") {
			t.Errorf("unrendered template markers in %s", f.Path)
		}
		if strings.Contains(strings.ToLower(string(f.Content)), "sqlite") {
			t.Errorf("%s mentions SQLite in a PostgreSQL project", f.Path)
		}
	}
	for path, want := range map[string]string{
		"gleam.toml":                                   `pog = ">= 4.1.0 and < 5.0.0"`,
		"docker-compose.yml":                           "POSTGRES_DB: demo_site_dev",
		"docker/postgres/init.sql":                     "CREATE DATABASE demo_site_test;",
		".env.example":                                 "DATABASE_URL=postgres://postgres:postgres@127.0.0.1:5432/demo_site_dev",
		"src/demo_site.gleam":                          "database.connect(url)",
		"src/demo_site/platform/database.gleam":        "pog.url_config",
		"src/demo_site/migrations/create_todos.gleam":  "pg.Boolean",
		"src/demo_site/features/todos/todos.gleam":     "sql.bool(done)",
		"src/demo_site/features/todos/todo_item.gleam": "decode.field(2, decode.bool)",
		"src/tasks/migrate.gleam":                      "`DATABASE_URL`",
		"test/demo_site/support.gleam":                 "TEST_DATABASE_URL",
		// Shared with the SQLite project.
		"src/demo_site/features/todos/todo_controller.gleam": "controller.new(\"api/todos\")",
	} {
		if !strings.Contains(byPath[path], want) {
			t.Errorf("%s should contain %q", path, want)
		}
	}
}

func TestParseRelease(t *testing.T) {
	for in, want := range map[string]string{"2.1.0": "2.1.0", "v2.1.0": "2.1.0", " 10.0.3 ": "10.0.3"} {
		if got, err := ParseRelease(in); err != nil || got != want {
			t.Errorf("ParseRelease(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "v2", "2.1", "main", "2.1.0-rc1", "2.1.0\"}"} {
		if _, err := ParseRelease(bad); err == nil {
			t.Errorf("ParseRelease(%q) should fail", bad)
		}
	}
	if _, err := ParseRelease(Release); err != nil {
		t.Errorf("the pinned release %q does not parse: %v", Release, err)
	}
}

// Every Howdy package in every kind of project comes from the chosen
// release, and nothing is left on the v2 branch.
func TestProjectsDependOnOneRelease(t *testing.T) {
	for _, db := range []Database{NoDatabase, SQLite, Postgres} {
		p, err := New(filepath.Join(t.TempDir(), "demo_site"))
		if err != nil {
			t.Fatal(err)
		}
		p.Database = db
		p.Release = "2.3.1"
		files, err := p.Files()
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			content := string(f.Content)
			if strings.Contains(content, `ref = "v2"`) || strings.Contains(content, "ref `v2`") {
				t.Errorf("%s (%s) still points at the v2 branch", f.Path, db.Label())
			}
			if f.Path == "gleam.toml" {
				refs := strings.Count(content, `ref = "v2.3.1"`)
				howdy := strings.Count(content, "mikeyjones/howdy.git")
				if refs == 0 || refs != howdy {
					t.Errorf("gleam.toml (%s): %d of %d Howdy packages on v2.3.1", db.Label(), refs, howdy)
				}
			}
			if f.Path == "README.md" && !strings.Contains(content, "This project uses Howdy 2.3.1") {
				t.Errorf("README.md (%s) does not say which release it uses", db.Label())
			}
		}
	}
}

// Every project gets the admin, telemetry and the console unless it is
// made with --minimal, whichever database it uses.
func TestFullProject(t *testing.T) {
	for _, db := range []Database{NoDatabase, SQLite, Postgres} {
		p, err := New(filepath.Join(t.TempDir(), "demo_site"))
		if err != nil {
			t.Fatal(err)
		}
		p.Database = db
		files, err := p.Files()
		if err != nil {
			t.Fatal(err)
		}
		byPath := map[string]string{}
		for _, f := range files {
			byPath[f.Path] = string(f.Content)
			if strings.Contains(string(f.Content), "{{") {
				t.Errorf("unrendered template markers in %s (%s)", f.Path, db.Label())
			}
		}
		for path, wants := range map[string][]string{
			"gleam.toml": {
				`howdy_telemetry = { git = "https://github.com/mikeyjones/howdy.git", ref = "v` + Release + `", path = "telemetry" }`,
				`howdy_admin = { git = "https://github.com/mikeyjones/howdy.git", ref = "v` + Release + `", path = "admin" }`,
				`gsh = ">= 1.3.0 and < 2.0.0"`,
				`imports = ["console", "gleam/list", "gleam/option", "gleam/result"]`,
			},
			"dev/demo_site_dev.gleam": {"admin.mount(dashboard)", "admin.telemetry(recorder)", "telemetry.record(recorder)"},
			"src/demo_site.gleam":     {"telemetry.from_env(\"demo_site\")", "start_telemetry()"},
			"src/console.gleam":       {"pub fn get(path: String)"},
			"README.md":               {"gleam run -m gsh -- demo_site_dev", "--remsh demo_site@localhost", "/_howdy"},
			".gitignore":              {"src/gsh_eval_*.gleam"},
		} {
			for _, want := range wants {
				if !strings.Contains(byPath[path], want) {
					t.Errorf("%s (%s) should contain %q", path, db.Label(), want)
				}
			}
		}
		// The admin is development only.
		toml := byPath["gleam.toml"]
		if strings.Index(toml, "howdy_admin") < strings.Index(toml, "[dev_dependencies]") {
			t.Errorf("gleam.toml (%s): howdy_admin should be a dev dependency", db.Label())
		}
		if db == NoDatabase {
			if strings.Contains(byPath["dev/demo_site_dev.gleam"], "admin.database") {
				t.Error("a page-only project has no database for the admin")
			}
			continue
		}
		for path, want := range map[string]string{
			"dev/demo_site_dev.gleam": "admin.database(db)",
			"src/demo_site.gleam":     "let services = Services(db:)",
			"src/console.gleam":       "exposed.get(demo_site.services_key())",
		} {
			if !strings.Contains(byPath[path], want) {
				t.Errorf("%s (%s) should contain %q", path, db.Label(), want)
			}
		}
		traced := map[Database]string{SQLite: "src/demo_site.gleam", Postgres: "src/demo_site/platform/database.gleam"}[db]
		if !strings.Contains(byPath[traced], "traced") {
			t.Errorf("%s (%s) should trace the Repo", traced, db.Label())
		}
	}
}

func TestMinimalProject(t *testing.T) {
	for _, db := range []Database{NoDatabase, SQLite, Postgres} {
		p, err := New(filepath.Join(t.TempDir(), "demo_site"))
		if err != nil {
			t.Fatal(err)
		}
		p.Database = db
		p.Minimal = true
		files, err := p.Files()
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			if f.Path == "src/console.gleam" {
				t.Errorf("a minimal project (%s) should not have a console", db.Label())
			}
			for _, word := range []string{"howdy_admin", "howdy/admin", "howdy_telemetry", "howdy/telemetry", "gsh", "_howdy"} {
				if strings.Contains(string(f.Content), word) {
					t.Errorf("%s in a minimal project (%s) mentions %q", f.Path, db.Label(), word)
				}
			}
		}
	}
}

// An --api-only project is a JSON backend: no Howdy UI, Lustre, pages,
// layouts or static files, and a health check in place of the home page.
func TestAPIProject(t *testing.T) {
	for _, minimal := range []bool{false, true} {
		for _, db := range []Database{NoDatabase, SQLite, Postgres} {
			label := db.Label()
			if minimal {
				label += ", minimal"
			}
			p, err := New(filepath.Join(t.TempDir(), "demo_site"))
			if err != nil {
				t.Fatal(err)
			}
			p.Database, p.Minimal, p.API = db, minimal, true
			files, err := p.Files()
			if err != nil {
				t.Fatal(err)
			}
			byPath := map[string]string{}
			for _, f := range files {
				content := string(f.Content)
				byPath[f.Path] = content
				if strings.Contains(content, "{{") {
					t.Errorf("unrendered template markers in %s (%s)", f.Path, label)
				}
				for _, prefix := range []string{"priv/", "src/demo_site/features/home/", "src/demo_site/layouts/", "test/demo_site/features/home/"} {
					if strings.HasPrefix(f.Path, prefix) {
						t.Errorf("an API project (%s) should not have %s", label, f.Path)
					}
				}
				for _, word := range []string{"howdy_ui", "howdy/ui", "lustre", "static.serve", "platform/not_found", "not_found_page", "home_controller", "priv/public", "tasks/css", "_layout"} {
					if strings.Contains(content, word) {
						t.Errorf("%s in an API project (%s) mentions %q", f.Path, label, word)
					}
				}
			}
			for _, gone := range []string{"src/tasks/css.gleam", "src/demo_site/platform/not_found.gleam"} {
				if _, ok := byPath[gone]; ok {
					t.Errorf("an API project (%s) should not have %s", label, gone)
				}
			}
			for path, wants := range map[string][]string{
				"gleam.toml":          {`description = "A Howdy API server`, `gleam_json = ">= 3.1.0 and < 4.0.0"`},
				"src/demo_site.gleam": {"|> howdy.controller(health.controller())", "import demo_site/features/health/health_controller as health", "|> howdy.not_found(fn(ctx) {", `service.NotFound("Not found")`},
				"src/demo_site/features/health/health_controller.gleam":       {`controller.new("health")`},
				"test/demo_site/features/health/health_controller_test.gleam": {`get("/health")`},
				"README.md":               {"howdy new --api-only", "curl http://127.0.0.1:8787/health"},
				"dev/demo_site_dev.gleam": {"dev.new(fn() {"},
			} {
				for _, want := range wants {
					if !strings.Contains(byPath[path], want) {
						t.Errorf("%s (%s) should contain %q:\n%s", path, label, want, byPath[path])
					}
				}
			}
			if db != NoDatabase {
				for path, want := range map[string]string{
					"src/demo_site.gleam":                      "|> howdy.controller(todos.controller(db))",
					"src/demo_site/features/todos/todos.gleam": "pub fn all(",
					"README.md": "`/api/todos`",
				} {
					if !strings.Contains(byPath[path], want) {
						t.Errorf("%s (%s) should contain %q", path, label, want)
					}
				}
			}
		}
	}
}
