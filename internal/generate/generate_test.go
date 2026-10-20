package generate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"howdy/internal/scaffold"
)

// project scaffolds a real project so the generator is tested against the
// files 'howdy new' writes.
func project(t *testing.T, database scaffold.Database) *Project {
	t.Helper()
	s, err := scaffold.New(filepath.Join(t.TempDir(), "demo_site"))
	if err != nil {
		t.Fatal(err)
	}
	s.Database = database
	// These tests check the Git dependency lines; see TestAddToHexProject.
	s.Source = scaffold.Git
	if _, err := s.Write(nil); err != nil {
		t.Fatal(err)
	}
	p, err := Load(s.Dir)
	if database == scaffold.NoDatabase {
		if err == nil {
			t.Fatal("a project without a database should be rejected")
		}
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func read(t *testing.T, p *Project, path string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(p.Dir, path))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestLoadDetectsDatabase(t *testing.T) {
	if p := project(t, scaffold.SQLite); p.Database != "sqlite" || p.Name != "demo_site" {
		t.Errorf("got %+v", p)
	}
	if p := project(t, scaffold.Postgres); p.Database != "postgres" {
		t.Errorf("got %+v", p)
	}
	project(t, scaffold.NoDatabase)
}

func TestNewControllerValidates(t *testing.T) {
	p := project(t, scaffold.SQLite)
	for _, ok := range [][]string{
		{"users", "user", "name:string", "age:int"},
		{"orders", "order", "total:float", "paid:bool"},
		{"posts", "post", "title"},
	} {
		if _, err := NewSlice(p, ok[0], ok[1], ok[2:]); err != nil {
			t.Errorf("%v should be valid: %v", ok, err)
		}
	}
	for _, bad := range [][]string{
		{"users", "user"},
		{"sheep", "sheep", "name"},
		{"Users", "user", "name"},
		{"order", "orders", "name"},
		{"types", "type", "name"},
		{"lists", "list", "name"},
		{"items", "item", "name"},
		{"databases", "database", "name"},
		{"users", "user", "age:date"},
		{"users", "user", "id:int"},
		{"users", "user", "order:int"},
		{"users", "user", "name", "name:int"},
	} {
		if _, err := NewSlice(p, bad[0], bad[1], bad[2:]); err == nil {
			t.Errorf("%v should be rejected", bad)
		}
	}
}

// A project scaffolded before howdy_database existed has no such dependency,
// so its contexts keep the helper that builds RETURNING themselves.
func TestReturningFollowsTheProjectsDependencies(t *testing.T) {
	for _, tc := range []struct {
		current     bool
		want, avoid string
	}{
		{true, "database.returning(", "query.to_sql("},
		{false, "query.to_sql(", "howdy/database"},
	} {
		p := project(t, scaffold.SQLite)
		if !p.HowdyDatabase {
			t.Fatal("a new project should depend on howdy_database")
		}
		p.HowdyDatabase = tc.current
		c, err := NewSlice(p, "users", "user", []string{"name"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.Write(); err != nil {
			t.Fatal(err)
		}
		context := read(t, p, "src/demo_site/features/users/users.gleam")
		if !strings.Contains(context, tc.want) || strings.Contains(context, tc.avoid) {
			t.Errorf("howdy_database=%v: context should contain %q and not %q:\n%s", tc.current, tc.want, tc.avoid, context)
		}
	}
}

func TestWriteAndRegister(t *testing.T) {
	p := project(t, scaffold.SQLite)
	c, err := NewSlice(p, "users", "user", []string{"name:string", "age:int", "admin:bool"})
	if err != nil {
		t.Fatal(err)
	}
	c.Routes = true
	if c.Version != 2 {
		t.Errorf("version = %d, want 2 after create_todos", c.Version)
	}
	written, err := c.Write()
	if err != nil {
		t.Fatal(err)
	}
	if len(written) != 7 {
		t.Errorf("wrote %v", written)
	}
	if notes := c.Register(); len(notes) != 0 {
		t.Errorf("registration needed a hand: %v", notes)
	}
	for path, want := range map[string]string{
		"src/demo_site/features/users/user.gleam":                   "User(id: Int, name: String, age: Int, admin: Bool)",
		"src/demo_site/features/users/users.gleam":                  "pub fn update(db: Repo, id: Int, input: NewUser)",
		"src/demo_site/features/users/users_controller.gleam":       `controller.new("api/users")`,
		"src/demo_site/migrations/create_users.gleam":               `sqlite.column("admin", sqlite.Integer)`,
		"test/demo_site/features/users/users_controller_test.gleam": `testing.put("/api/users/1", changed())`,
		"src/demo_site.gleam":                                       "|> howdy.controller(users_controller.controller(db))",
		"src/demo_site/platform/database.gleam":                     "create_todos.migration(), create_users.migration()]",
	} {
		content := read(t, p, path)
		if !strings.Contains(content, want) {
			t.Errorf("%s should contain %q", path, want)
		}
		if strings.Contains(content, "{{") {
			t.Errorf("unrendered template markers in %s", path)
		}
	}
	for _, imp := range []string{"import demo_site/features/users/users_controller\n"} {
		if !strings.Contains(read(t, p, "src/demo_site.gleam"), imp) {
			t.Errorf("src/demo_site.gleam should contain %q", imp)
		}
	}
	if !strings.Contains(read(t, p, "src/demo_site/platform/database.gleam"), "import demo_site/migrations/create_users\n") {
		t.Error("database.gleam should import the migration")
	}

	// A second slice takes the next version and joins the list.
	c2, err := NewSlice(p, "orders", "order", []string{"total:float"})
	if err != nil {
		t.Fatal(err)
	}
	c2.Routes = true
	if c2.Version != 3 {
		t.Errorf("version = %d, want 3", c2.Version)
	}
	if _, err := c2.Write(); err != nil {
		t.Fatal(err)
	}
	if notes := c2.Register(); len(notes) != 0 {
		t.Errorf("registration needed a hand: %v", notes)
	}
	if !strings.Contains(read(t, p, "src/demo_site/platform/database.gleam"), "create_users.migration(), create_orders.migration()]") {
		t.Error("second migration was not appended")
	}
	if !strings.Contains(read(t, p, "src/demo_site/features/orders/order.gleam"), "fn number()") {
		t.Error("float fields need the lenient JSON decoder")
	}

	// Running it again must not overwrite anything.
	if _, err := c.Write(); err == nil {
		t.Error("existing files should not be overwritten")
	}
}

func TestRepoHasNoController(t *testing.T) {
	p := project(t, scaffold.SQLite)
	before := read(t, p, "src/demo_site.gleam")
	c, err := NewSlice(p, "users", "user", []string{"name", "age:int"})
	if err != nil {
		t.Fatal(err)
	}
	written, err := c.Write()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"src/demo_site/features/users/user.gleam", "src/demo_site/features/users/users.gleam",
		"dev/demo_site/factories/user_factory.gleam", "src/demo_site/migrations/create_users.gleam",
		"test/demo_site/features/users/users_test.gleam",
	}
	if strings.Join(written, " ") != strings.Join(want, " ") {
		t.Errorf("wrote %v, want %v", written, want)
	}
	if notes := c.Register(); len(notes) != 0 {
		t.Errorf("registration needed a hand: %v", notes)
	}
	if read(t, p, "src/demo_site.gleam") != before {
		t.Error("a repo should not touch the app")
	}
	if !strings.Contains(read(t, p, "src/demo_site/platform/database.gleam"), "create_users.migration()]") {
		t.Error("the migration should be registered")
	}
	if !strings.Contains(read(t, p, "test/demo_site/features/users/users_test.gleam"), `user.UserInput(name: "First name", age: 1)`) {
		t.Error("the context test should build fixtures through validate")
	}
}

func TestFactory(t *testing.T) {
	p := project(t, scaffold.SQLite)
	c, err := NewSlice(p, "users", "user", []string{"name", "age:int", "score:float", "admin:bool"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Write(); err != nil {
		t.Fatal(err)
	}
	factory := read(t, p, "dev/demo_site/factories/user_factory.gleam")
	for _, want := range []string{
		"import demo_site/features/users/user.{type User, type UserInput}",
		`user.UserInput(name: "Name " <> n, age: 1, score: 1.5, admin: False)`,
		"let assert Ok(valid) = user.validate(input)",
		"let assert Ok(item) = users.create(db, valid)",
		"pub fn insert_many(db: Repo, count: Int, with: fn(UserInput) -> UserInput) -> List(User)",
		"unique_integer([Positive, Monotonic])",
	} {
		if !strings.Contains(factory, want) {
			t.Errorf("the factory should contain %q:\n%s", want, factory)
		}
	}

	// Without a string to number, the factory needs no sequence, which
	// would otherwise be an unused function.
	c, err = NewSlice(p, "counters", "counter", []string{"hits:int", "enabled:bool"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Write(); err != nil {
		t.Fatal(err)
	}
	factory = read(t, p, "dev/demo_site/factories/counter_factory.gleam")
	if !strings.Contains(factory, "counter.CounterInput(hits: 1, enabled: False)") {
		t.Errorf("unexpected input:\n%s", factory)
	}
	if strings.Contains(factory, "sequence") || strings.Contains(factory, "gleam/int") {
		t.Errorf("a factory without strings should not number them:\n%s", factory)
	}

	// A migration on its own has no record to build.
	m, err := NewMigration(p, "events", []string{"name:string"})
	if err != nil {
		t.Fatal(err)
	}
	written, err := m.Write()
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range written {
		if strings.HasPrefix(path, "dev/") {
			t.Errorf("a migration should not write %s", path)
		}
	}
}

func TestPages(t *testing.T) {
	p := project(t, scaffold.SQLite)
	c, err := NewSlice(p, "blog_posts", "blog_post", []string{"title", "views:int", "rating:float", "published:bool"})
	if err != nil {
		t.Fatal(err)
	}
	c.Pages = true
	written, err := c.Write()
	if err != nil {
		t.Fatal(err)
	}
	if len(written) != 14 {
		t.Errorf("wrote %v", written)
	}
	if notes := c.Register(); len(notes) != 0 {
		t.Errorf("registration needed a hand: %v", notes)
	}
	for path, want := range map[string]string{
		"src/demo_site/features/blog_posts/blog_posts_controller.gleam":       `controller.new("blog_posts")`,
		"src/demo_site/features/blog_posts/pages/index_page.gleam":            `helpers.yes_no(item.published)`,
		"src/demo_site/features/blog_posts/pages/show_page.gleam":             `"Back to blog posts"`,
		"src/demo_site/features/blog_posts/pages/new_page.gleam":              `"New blog post"`,
		"src/demo_site/features/blog_posts/pages/edit_page.gleam":             `"Edit blog post"`,
		"src/demo_site/platform/pages.gleam":                                  "pub fn read_form(",
		"priv/public/assets/style.css":                                        ".page-table",
		"src/demo_site/platform/pages.gleam#":                                 "default_layout.render(ctx, \"Something went wrong\"",
		"test/demo_site/features/blog_posts/blog_posts_controller_test.gleam": `#("title", "First title"), #("views", "1"), #("rating", "1.5")`,
		"src/demo_site.gleam":                                                 "|> howdy.controller(blog_posts_controller.controller(db))",
	} {
		content := read(t, p, strings.TrimSuffix(path, "#"))
		if !strings.Contains(content, want) {
			t.Errorf("%s should contain %q", path, want)
		}
		if strings.Contains(content, "{{") {
			t.Errorf("unrendered template markers in %s", path)
		}
	}
	helpers := read(t, p, "src/demo_site/features/blog_posts/pages/helpers.gleam")
	for _, want := range []string{
		`use views <- pages.whole_number(form, "views")`,
		`use rating <- pages.number(form, "rating")`,
		`let published = pages.checked(form, "published")`,
		`pages.checkbox(form, "published", "Published")`,
		`pub fn yes_no(`,
	} {
		if !strings.Contains(helpers, want) {
			t.Errorf("the pages' helpers should contain %q", want)
		}
	}
	if strings.Contains(read(t, p, "src/demo_site.gleam"), "blog_posts_api_controller") {
		t.Error("pages alone should not register a JSON controller")
	}

	// A second set of pages keeps the shared helpers, which may have been edited.
	shared := filepath.Join(p.Dir, "src", "demo_site", "platform", "pages.gleam")
	os.WriteFile(shared, []byte("// mine\n"), 0o644)
	c2, err := NewSlice(p, "users", "user", []string{"admin:bool"})
	if err != nil {
		t.Fatal(err)
	}
	c2.Pages = true
	written, err = c2.Write()
	if err != nil {
		t.Fatal(err)
	}
	if len(written) != 12 {
		t.Errorf("wrote %v", written)
	}
	if got, _ := os.ReadFile(shared); string(got) != "// mine\n" {
		t.Error("existing shared helpers should be kept")
	}
	if !strings.Contains(read(t, p, "src/demo_site/features/users/pages/helpers.gleam"), "_errors: List(service.FieldError)") {
		t.Error("checkbox-only pages should not leave errors unused")
	}
}

func TestMigrationOnly(t *testing.T) {
	p := project(t, scaffold.Postgres)
	c, err := NewMigration(p, "users", []string{"name:string", "age:int"})
	if err != nil {
		t.Fatal(err)
	}
	written, err := c.Write()
	if err != nil {
		t.Fatal(err)
	}
	if len(written) != 1 || written[0] != "src/demo_site/migrations/create_users.gleam" {
		t.Errorf("wrote %v", written)
	}
	if notes := c.Register(); len(notes) != 0 {
		t.Errorf("registration needed a hand: %v", notes)
	}
	migration := read(t, p, written[0])
	for _, want := range []string{"version: 2", `table: "users"`, `pg.column("age", pg.BigInt)`} {
		if !strings.Contains(migration, want) {
			t.Errorf("migration should contain %q", want)
		}
	}
	if !strings.Contains(read(t, p, "src/demo_site/platform/database.gleam"), "create_users.migration()]") {
		t.Error("the migration should be registered")
	}
	// The singular that the other commands take is not a column.
	if _, err := NewMigration(p, "posts", []string{"post", "title:string"}); err == nil {
		t.Error("a bare word should be rejected")
	}
	if _, err := NewMigration(p, "order", []string{"total:int"}); err == nil {
		t.Error("an SQL keyword should not name the table")
	}
}

func TestPostgresColumns(t *testing.T) {
	p := project(t, scaffold.Postgres)
	c, err := NewSlice(p, "users", "user", []string{"admin:bool"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Write(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(t, p, "src/demo_site/migrations/create_users.gleam"), `pg.column("admin", pg.Boolean)`) {
		t.Error("migration should use Postgres column types")
	}
	if strings.Contains(read(t, p, "src/demo_site/features/users/user.gleam"), "n != 0") {
		t.Error("Postgres booleans decode directly")
	}
}

func TestRegisterFallsBackToNotes(t *testing.T) {
	p := project(t, scaffold.SQLite)
	root := filepath.Join(p.Dir, "src", "demo_site.gleam")
	os.WriteFile(root, []byte("pub fn main() { Nil }\n"), 0o644)
	c, err := NewSlice(p, "users", "user", []string{"name"})
	if err != nil {
		t.Fatal(err)
	}
	c.Routes = true
	notes := c.Register()
	if len(notes) != 1 || !strings.Contains(notes[0], "users_controller") {
		t.Errorf("notes = %v", notes)
	}
	if got, _ := os.ReadFile(root); string(got) != "pub fn main() { Nil }\n" {
		t.Error("an unrecognised file should be left alone")
	}
}
