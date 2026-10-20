package scaffold

import (
	"fmt"
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
	p.Database, p.Source = NoDatabase, Git

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
		`howdy = { git = "https://github.com/howdy-gleam/howdy.git", ref = "v` + Release + `" }`,
		`howdy_ui = { git = "https://github.com/howdy-gleam/howdy.git", ref = "v` + Release + `", path = "ui" }`,
		`howdy_dev = { git = "https://github.com/howdy-gleam/howdy.git", ref = "v` + Release + `", path = "howdy_dev" }`,
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
	if _, err := os.Stat(filepath.Join(p.Dir, "dev/seed.gleam")); err == nil {
		t.Error("a project without a database has nothing to seed")
	}
}

func TestDatabaseOverlay(t *testing.T) {
	p, err := New(filepath.Join(t.TempDir(), "demo_site"))
	if err != nil {
		t.Fatal(err)
	}
	if p.Database != Postgres {
		t.Fatal("database should be PostgreSQL by default")
	}
	p.Database = SQLite
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
		"src/tasks/migrate.gleam", "test/demo_site/support.gleam",
		"priv/public/assets/ui.css", "src/tasks/css.gleam",
		"dev/seed.gleam", "dev/seed/reset.gleam", "dev/demo_site/factories/todo_factory.gleam",
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
	if _, ok := byPath["test/demo_site_test_ffi.erl"]; ok {
		t.Error("the tests should close the database with howdy/testing.cleanup, not an Erlang module")
	}
	if !strings.Contains(byPath["test/demo_site/support.gleam"], "use <- testing.cleanup(") {
		t.Error("the tests should close the database with howdy/testing.cleanup")
	}
	if _, ok := byPath["docker-compose.yml"]; ok {
		t.Error("a SQLite project should not get a docker-compose.yml")
	}
	if !strings.Contains(byPath["dev/seed.gleam"], "todo_factory.insert_many(db, 3,") {
		t.Error("the seeds should insert todos through their factory")
	}
	if !strings.Contains(byPath["dev/seed/reset.gleam"], "FROM sqlite_master") {
		t.Error("a SQLite reset should drop the tables sqlite_master lists")
	}
	for path, content := range byPath {
		if strings.Contains(strings.ToLower(content), "postgres") {
			t.Errorf("%s mentions postgres in a SQLite project", path)
		}
	}
}

func TestSeedFiles(t *testing.T) {
	for _, database := range []Database{SQLite, Postgres} {
		files, err := SeedFiles("demo_site", database)
		if err != nil {
			t.Fatal(err)
		}
		if len(files) != 2 || files[0].Path != "dev/seed.gleam" || files[1].Path != "dev/seed/reset.gleam" {
			t.Fatalf("files = %v", files)
		}
		seed := string(files[0].Content)
		if strings.Contains(seed, "todo") || !strings.Contains(seed, "Nothing to seed yet") {
			t.Errorf("seeds for an existing project should not assume the todo list:\n%s", seed)
		}
		for _, f := range files {
			if strings.Contains(string(f.Content), "{{") {
				t.Errorf("unrendered template markers in %s", f.Path)
			}
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
		"dev/seed.gleam":                               "`DATABASE_URL`",
		"dev/seed/reset.gleam":                         "DROP SCHEMA IF EXISTS public CASCADE",
		"test/demo_site/support.gleam":                 "TEST_DATABASE_URL",
		// Shared with the SQLite project.
		"src/demo_site/features/todos/todo_controller.gleam": "controller.new(\"api/todos\")",
	} {
		if !strings.Contains(byPath[path], want) {
			t.Errorf("%s should contain %q", path, want)
		}
	}
	// Emptying the tables would lose the migration records and seed rows
	// that Howdy packages such as auth and flags keep in the database.
	support := byPath["test/demo_site/support.gleam"]
	if !strings.Contains(support, "DROP SCHEMA IF EXISTS public CASCADE") || strings.Contains(support, "TRUNCATE") {
		t.Errorf("test support should drop and remake the schema before each test, not truncate its tables")
	}
}

func TestParseRelease(t *testing.T) {
	for in, want := range map[string]string{"1.0.0": "1.0.0", "v1.0.0": "1.0.0", " 10.0.3 ": "10.0.3"} {
		if got, err := ParseRelease(in); err != nil || got != want {
			t.Errorf("ParseRelease(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "v1", "1.0", "main", "1.0.0-rc1", "1.0.0\"}"} {
		if _, err := ParseRelease(bad); err == nil {
			t.Errorf("ParseRelease(%q) should fail", bad)
		}
	}
	if _, err := ParseRelease(Release); err != nil {
		t.Errorf("the pinned release %q does not parse: %v", Release, err)
	}
}

// Every Howdy package in every kind of project comes from the chosen
// release, and nothing is left on a branch such as v1 or v2.
func TestProjectsDependOnOneRelease(t *testing.T) {
	for _, db := range []Database{NoDatabase, SQLite, Postgres} {
		p, err := New(filepath.Join(t.TempDir(), "demo_site"))
		if err != nil {
			t.Fatal(err)
		}
		p.Database = db
		p.Release, p.Source = "2.3.1", Git
		files, err := p.Files()
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			content := string(f.Content)
			for _, branch := range []string{"v1", "v2"} {
				if strings.Contains(content, `ref = "`+branch+`"`) || strings.Contains(content, "ref `"+branch+"`") {
					t.Errorf("%s (%s) still points at the %s branch", f.Path, db.Label(), branch)
				}
			}
			if f.Path == "gleam.toml" {
				refs := strings.Count(content, `ref = "v2.3.1"`)
				howdy := strings.Count(content, "howdy-gleam/howdy.git")
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

func TestRepositoryReplacesTheHowdyRepository(t *testing.T) {
	p, err := New(filepath.Join(t.TempDir(), "demo_site"))
	if err != nil {
		t.Fatal(err)
	}
	p.Source, p.Repository = Git, "https://git.example.com/me/howdy.git"
	files, err := p.Files()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		content := string(f.Content)
		if strings.Contains(content, "howdy-gleam/howdy.git") {
			t.Errorf("%s still takes packages from %s", f.Path, Repository)
		}
		if f.Path == "gleam.toml" && !strings.Contains(content, `howdy = { git = "https://git.example.com/me/howdy.git", ref = "v`+Release+`" }`) {
			t.Errorf("gleam.toml should take howdy from the repository:\n%s", content)
		}
		if f.Path == "README.md" && !strings.Contains(content, "from `https://git.example.com/me/howdy.git` at the Howdy") {
			t.Error("README.md should say where the packages come from")
		}
	}
}

// Every project can be published to a hosting platform as it is made: it
// listens on HOST, which a container sets to 0.0.0.0, and finds its public
// files through the application, so an exported shipment started from
// another directory still serves them.
func TestProjectsRunInAContainer(t *testing.T) {
	for _, api := range []bool{false, true} {
		for _, db := range []Database{NoDatabase, SQLite, Postgres} {
			label := db.Label()
			if api {
				label += ", API"
			}
			p, err := New(filepath.Join(t.TempDir(), "demo_site"))
			if err != nil {
				t.Fatal(err)
			}
			p.Database, p.API = db, api
			files, err := p.Files()
			if err != nil {
				t.Fatal(err)
			}
			byPath := map[string]string{}
			for _, f := range files {
				byPath[f.Path] = string(f.Content)
			}
			root := byPath["src/demo_site.gleam"]
			for _, want := range []string{`env.get_string_or("HOST", "127.0.0.1")`, "howdy.bind(host)"} {
				if !strings.Contains(root, want) {
					t.Errorf("src/demo_site.gleam (%s) is missing %q", label, want)
				}
			}
			if !api && !strings.Contains(root, `application.priv_directory("demo_site")`) {
				t.Errorf("src/demo_site.gleam (%s) does not find priv/public through the application", label)
			}
			if !strings.Contains(byPath[".env.example"], "HOST=") {
				t.Errorf(".env.example (%s) does not mention HOST", label)
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
		p.Database, p.Source = db, Git
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
				`howdy_telemetry = { git = "https://github.com/howdy-gleam/howdy.git", ref = "v` + Release + `", path = "telemetry" }`,
				`howdy_admin = { git = "https://github.com/howdy-gleam/howdy.git", ref = "v` + Release + `", path = "admin" }`,
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
// layouts or static files.
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
				"gleam.toml":                             {`description = "A Howdy API server`, `gleam_json = ">= 3.1.0 and < 4.0.0"`},
				"src/demo_site.gleam":                    {"|> howdy.health_check(health.new())", "|> howdy.not_found(fn(ctx) {", `service.NotFound("Not found")`},
				"test/demo_site/health_check_test.gleam": {`testing.get("/up")`},
				"test/demo_site/not_found_test.gleam":    {`testing.get("/no/such/route")`},
				"README.md":                              {"howdy new --api-only", "curl http://127.0.0.1:8787/up"},
				"dev/demo_site_dev.gleam":                {"dev.new(fn() {"},
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

func TestAgentGuide(t *testing.T) {
	for _, api := range []bool{false, true} {
		for _, minimal := range []bool{false, true} {
			for _, db := range []Database{NoDatabase, SQLite, Postgres} {
				label := fmt.Sprintf("%s, api %t, minimal %t", db.Label(), api, minimal)
				p, err := New(filepath.Join(t.TempDir(), "demo_site"))
				if err != nil {
					t.Fatal(err)
				}
				p.Database, p.Minimal, p.API = db, minimal, api
				files, err := p.Files()
				if err != nil {
					t.Fatal(err)
				}
				byPath := map[string]string{}
				for _, f := range files {
					byPath[f.Path] = string(f.Content)
				}
				if byPath["CLAUDE.md"] != "@AGENTS.md\n" {
					t.Errorf("CLAUDE.md (%s) should import AGENTS.md: %q", label, byPath["CLAUDE.md"])
				}
				guide := byPath["AGENTS.md"]
				for _, want := range []string{"howdy check", "howdy routes", "--dry-run", "src/demo_site.gleam", "release " + Release} {
					if !strings.Contains(guide, want) {
						t.Errorf("AGENTS.md (%s) should mention %q", label, want)
					}
				}
				has := func(s string) bool { return strings.Contains(guide, s) }
				if has("howdy create controller") != (db != NoDatabase) {
					t.Errorf("AGENTS.md (%s) should offer create controller only with a database", label)
				}
				if has("howdy create page ") == api {
					t.Errorf("AGENTS.md (%s) should offer create page only with pages", label)
				}
				if has("/_howdy") == minimal {
					t.Errorf("AGENTS.md (%s) should mention the admin only when it has one", label)
				}
				if has("docker compose") != (db == Postgres) {
					t.Errorf("AGENTS.md (%s) should mention docker compose only for PostgreSQL", label)
				}
				if strings.Contains(guide, "\n\n\n") || strings.Contains(guide, "<no value>") {
					t.Errorf("AGENTS.md (%s) is badly rendered:\n%s", label, guide)
				}

				skills := map[string]bool{}
				for path, content := range byPath {
					dir, file, ok := strings.Cut(strings.TrimPrefix(path, ".claude/skills/"), "/")
					if !strings.HasPrefix(path, ".claude/skills/") || !ok {
						continue
					}
					if strings.Contains(content, "\n\n\n") || strings.Contains(content, "<no value>") {
						t.Errorf("%s (%s) is badly rendered:\n%s", path, label, content)
					}
					if file != "SKILL.md" {
						continue
					}
					skills[dir] = true
					front, _, ok := strings.Cut(strings.TrimPrefix(content, "---\n"), "\n---\n")
					if !strings.HasPrefix(content, "---\n") || !ok {
						t.Errorf("%s (%s) has no frontmatter", path, label)
						continue
					}
					if !strings.Contains(front, "\nname: "+dir+"\n") && !strings.HasPrefix(front, "name: "+dir+"\n") {
						t.Errorf("%s (%s) should be named %s:\n%s", path, label, dir, front)
					}
					if !strings.Contains(front, "\ndescription: ") {
						t.Errorf("%s (%s) has no description", path, label)
					}
					if !strings.Contains(guide, "`"+dir+"/SKILL.md`") {
						t.Errorf("AGENTS.md (%s) should list the skill %s", label, dir)
					}
				}
				want := []string{"howdy-feature", "howdy-controllers", "howdy-testing", "howdy-debugging"}
				if db != NoDatabase {
					want = append(want, "howdy-database")
				}
				if !api {
					want = append(want, "howdy-pages")
				}
				if len(skills) != len(want) {
					t.Errorf("skills (%s) = %v, want %v", label, skills, want)
				}
				for _, w := range want {
					if !skills[w] {
						t.Errorf("missing the skill %s (%s)", w, label)
					}
				}
			}
		}
	}
}

func TestNoAgents(t *testing.T) {
	for _, api := range []bool{false, true} {
		p, err := New(filepath.Join(t.TempDir(), "demo_site"))
		if err != nil {
			t.Fatal(err)
		}
		p.API, p.NoAgents = api, true
		files, err := p.Files()
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			if forAgents(f.Path) || strings.HasPrefix(f.Path, ".claude/") {
				t.Errorf("--no-agents (api %t) should leave out %s", api, f.Path)
			}
		}
		if len(files) < 10 {
			t.Errorf("--no-agents (api %t) should keep the rest of the project: %d files", api, len(files))
		}
	}
}

// Every project is ready to run in production: GET /up for health checks,
// a server that drains on SIGTERM, and migrations that replicas starting
// together take turns to apply.
func TestProjectsAreReadyToDeploy(t *testing.T) {
	for _, api := range []bool{false, true} {
		for _, db := range []Database{NoDatabase, SQLite, Postgres} {
			label := db.Label()
			if api {
				label += ", API"
			}
			p, err := New(filepath.Join(t.TempDir(), "demo_site"))
			if err != nil {
				t.Fatal(err)
			}
			p.Database, p.API = db, api
			files, err := p.Files()
			if err != nil {
				t.Fatal(err)
			}
			byPath := map[string]string{}
			for _, f := range files {
				byPath[f.Path] = string(f.Content)
			}
			wants := map[string][]string{
				"src/demo_site.gleam": {
					"|> howdy.health_check(health.new())",
					"|> howdy.shutdown_timeout(8000)",
					"|> supervisor.add(howdy.supervised(configured(services)))",
					`|> howdy.start_application(name: "demo_site_server")`,
				},
				"gleam.toml":                             {`gleam_otp = ">= 1.2.0 and < 2.0.0"`},
				"test/demo_site/health_check_test.gleam": {`testing.get("/up")`},
				"README.md":                              {"## Deploy", "`GET /up`"},
			}
			if db != NoDatabase {
				wants["src/demo_site.gleam"] = append(wants["src/demo_site.gleam"], `env.get_bool_or("MIGRATE_ON_START", True)`)
				wants["src/demo_site/platform/database.gleam"] = []string{"use db <- one_at_a_time(db)", "howdy_database.lock_migrations(conn)"}
				wants[".env.example"] = []string{"MIGRATE_ON_START"}
			}
			for path, ws := range wants {
				for _, want := range ws {
					if !strings.Contains(byPath[path], want) {
						t.Errorf("%s (%s) should contain %q", path, label, want)
					}
				}
			}
			if strings.Contains(byPath["src/demo_site.gleam"], "|> howdy.start\n") {
				t.Errorf("src/demo_site.gleam (%s) still starts the server outside an application", label)
			}
		}
	}
}

// An API project has no Howdy UI, so no islands to bundle: it exports the
// shipment with Gleam alone.
func TestDockerfileForAPI(t *testing.T) {
	p, err := New(filepath.Join(t.TempDir(), "demo_api"))
	if err != nil {
		t.Fatal(err)
	}
	p.Database, p.API = Postgres, true
	if _, err := p.Write(nil); err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile(filepath.Join(p.Dir, "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	dockerfile := string(src)
	if !strings.Contains(dockerfile, "RUN gleam export erlang-shipment") || strings.Contains(dockerfile, "howdy/ui") {
		t.Errorf("an API project's Dockerfile should export the shipment with Gleam alone:\n%s", dockerfile)
	}
}

func TestDockerFiles(t *testing.T) {
	for _, db := range []Database{NoDatabase, SQLite, Postgres} {
		p, err := New(filepath.Join(t.TempDir(), "demo_site"))
		if err != nil {
			t.Fatal(err)
		}
		p.Database, p.Gleam, p.Erlang = db, "1.18.1", "28"
		if _, err := p.Write(nil); err != nil {
			t.Fatal(err)
		}
		read := func(path string) string {
			src, err := os.ReadFile(filepath.Join(p.Dir, path))
			if err != nil {
				t.Errorf("%s (%s): %v", path, db.Label(), err)
			}
			return string(src)
		}
		dockerfile := read("Dockerfile")
		for _, want := range []string{
			"ARG GLEAM_VERSION=v1.18.1", "ARG ERLANG_VERSION=28",
			"gleam run -m howdy/ui export;", "gleam export erlang-shipment;",
			"COPY --chown=app bin/start /app/bin/start",
			"wget -qO /dev/null http://127.0.0.1:8787/up", `ENTRYPOINT ["/app/bin/start"]`,
		} {
			if !strings.Contains(dockerfile, want) {
				t.Errorf("Dockerfile (%s) should contain %q", db.Label(), want)
			}
		}
		// Gloo brings a native driver, whichever database the project uses.
		if got := strings.Contains(dockerfile, "build-base"); got != (db != NoDatabase) {
			t.Errorf("Dockerfile (%s) installs build-base: %v", db.Label(), got)
		}
		if got := strings.Contains(dockerfile, "VOLUME /data"); got != (db == SQLite) {
			t.Errorf("Dockerfile (%s) has a /data volume: %v", db.Label(), got)
		}
		start := read("bin/start")
		if got := strings.Contains(start, "tasks@migrate"); got != (db != NoDatabase) {
			t.Errorf("bin/start (%s) can migrate: %v", db.Label(), got)
		}
		if !strings.Contains(start, "PACKAGE=demo_site") || !strings.HasPrefix(start, "#!/bin/sh\n") {
			t.Errorf("bin/start (%s) does not start demo_site", db.Label())
		}
		if info, err := os.Stat(filepath.Join(p.Dir, "bin/start")); err != nil || info.Mode().Perm() != 0o755 {
			t.Errorf("bin/start (%s) should be executable", db.Label())
		}
		if !strings.Contains(read(".dockerignore"), "\n.env\n") {
			t.Errorf(".dockerignore (%s) should keep .env out of the image", db.Label())
		}
		if db == Postgres && !strings.Contains(read("docker-compose.yml"), "profiles: [app]") {
			t.Error("docker-compose.yml should offer the app under a profile")
		}
	}
}

func TestNoDocker(t *testing.T) {
	p, err := New(filepath.Join(t.TempDir(), "demo_site"))
	if err != nil {
		t.Fatal(err)
	}
	p.Database, p.NoDocker = Postgres, true
	files, err := p.Files()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		switch f.Path {
		case "Dockerfile", ".dockerignore", "bin/start":
			t.Errorf("--no-docker should leave out %s", f.Path)
		}
		for _, word := range []string{"bin/start", "Dockerfile", "profiles:"} {
			if strings.Contains(string(f.Content), word) {
				t.Errorf("%s mentions %q without the Docker files", f.Path, word)
			}
		}
	}
}
