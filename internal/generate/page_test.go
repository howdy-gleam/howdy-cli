package generate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"howdy/internal/scaffold"
)

// opened scaffolds a project of any kind, including one without a database.
func opened(t *testing.T, database scaffold.Database) *Project {
	t.Helper()
	s, err := scaffold.New(filepath.Join(t.TempDir(), "demo_site"))
	if err != nil {
		t.Fatal(err)
	}
	s.Database = database
	if _, err := s.Write(nil); err != nil {
		t.Fatal(err)
	}
	p, err := Open(s.Dir)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func createPage(t *testing.T, p *Project, controller, name string, live bool) *Page {
	t.Helper()
	pg, err := NewPage(p, controller, name, live, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pg.Write(); err != nil {
		t.Fatal(err)
	}
	if notes := pg.Register(); len(notes) > 0 {
		t.Fatalf("registration needs a hand: %v", notes)
	}
	return pg
}

func TestPageCreatesController(t *testing.T) {
	for _, db := range []scaffold.Database{scaffold.NoDatabase, scaffold.SQLite, scaffold.Postgres} {
		p := opened(t, db)
		pg := createPage(t, p, "blog", "about", false)
		if pg.Exists || pg.URL() != "/blog/about" {
			t.Errorf("%s: got %+v, URL %s", db.Label(), pg, pg.URL())
		}
		controller := read(t, p, "src/demo_site/blog/blog_controller.gleam")
		for _, want := range []string{
			`controller.new("blog")`,
			`|> controller.get("/about", about_page.show)`,
			"import demo_site/blog/pages/about_page\n",
		} {
			if !strings.Contains(controller, want) {
				t.Errorf("%s: blog_controller.gleam should contain %q:\n%s", db.Label(), want, controller)
			}
		}
		root := read(t, p, "src/demo_site.gleam")
		if !strings.Contains(root, "|> howdy.controller(blog_controller.controller())") ||
			!strings.Contains(root, "import demo_site/blog/blog_controller\n") {
			t.Errorf("%s: the controller is not registered:\n%s", db.Label(), root)
		}
		page := read(t, p, "src/demo_site/blog/pages/about_page.gleam")
		if !strings.Contains(page, `default_layout.render(ctx, "About", [`) || strings.Contains(page, "render_live") {
			t.Errorf("%s: unexpected page:\n%s", db.Label(), page)
		}
		test := read(t, p, "test/demo_site/blog/pages/about_page_test.gleam")
		app := "support.app(db)"
		if db == scaffold.NoDatabase {
			app = "support.app()"
		}
		if !strings.Contains(test, `testing.get("/blog/about") |> testing.send(`+app+`)`) {
			t.Errorf("%s: unexpected test:\n%s", db.Label(), test)
		}
	}
}

func TestPageAddsToExistingController(t *testing.T) {
	p := opened(t, scaffold.SQLite)
	createPage(t, p, "blog", "about", false)
	root := read(t, p, "src/demo_site.gleam")
	pg := createPage(t, p, "blog", "editor_page", true)
	if !pg.Exists || pg.Module() != "editor_page" {
		t.Errorf("got %+v", pg)
	}
	controller := read(t, p, "src/demo_site/blog/blog_controller.gleam")
	for _, want := range []string{
		`|> controller.get("/about", about_page.show)` + "\n" + `|> controller.get("/editor", editor_page.show)` + "\n" +
			`|> controller.get("/editor/live", editor_page.socket)` + "\n}",
		"import demo_site/blog/pages/editor_page\n",
	} {
		if !strings.Contains(controller, want) {
			t.Errorf("blog_controller.gleam should contain %q:\n%s", want, controller)
		}
	}
	if read(t, p, "src/demo_site.gleam") != root {
		t.Error("an existing controller should not be registered again")
	}
	page := read(t, p, "src/demo_site/blog/pages/editor_page.gleam")
	if !strings.Contains(page, `live.mount("/blog/editor/live")`) || !strings.Contains(page, "pub fn socket(") ||
		!strings.Contains(page, `default_layout.render_live(ctx, "Editor", [`) {
		t.Errorf("unexpected live page:\n%s", page)
	}
	if !strings.Contains(read(t, p, "test/demo_site/blog/pages/editor_page_test.gleam"), `testing.get("/blog/editor/live")`) {
		t.Error("the live page test should check the socket route")
	}

	// The site controller from 'howdy new' is mounted at the root and takes
	// the database.
	site := createPage(t, p, "site", "contact", false)
	if site.URL() != "/contact" {
		t.Errorf("URL = %s", site.URL())
	}
	if !strings.Contains(read(t, p, "src/demo_site/site/site_controller.gleam"), `|> controller.get("/contact", contact_page.show)`+"\n}") {
		t.Error("the page was not added to the site controller")
	}
}

func TestIndexPage(t *testing.T) {
	p := opened(t, scaffold.NoDatabase)
	pg := createPage(t, p, "docs", "index", true)
	if pg.URL() != "/docs" || pg.SocketURL() != "/docs/live" {
		t.Errorf("URL = %s, socket %s", pg.URL(), pg.SocketURL())
	}
	if !strings.Contains(read(t, p, "src/demo_site/docs/docs_controller.gleam"), `controller.get("/", index_page.show)`) {
		t.Error("an index page should be at the controller's root")
	}
}

func TestPageRefusesDuplicates(t *testing.T) {
	p := opened(t, scaffold.SQLite)
	createPage(t, p, "blog", "about", false)
	before := read(t, p, "src/demo_site/blog/blog_controller.gleam")
	for _, c := range []struct {
		controller, name string
		live             bool
		want             string
	}{
		{"blog", "about", false, `the page "about" already exists in the blog controller`},
		{"blog", "about_page", true, `the page "about" already exists in the blog controller`},
		// home is routed by the site controller but has no page file.
		{"site", "index", false, "the site controller already has a page at /, which is where an index page goes"},
	} {
		_, err := NewPage(p, c.controller, c.name, c.live, "")
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s %s: got %v, want %q", c.controller, c.name, err, c.want)
		}
	}
	// A route added by hand counts too.
	path := filepath.Join(p.Dir, "src/demo_site/blog/blog_controller.gleam")
	os.WriteFile(path, []byte(strings.Replace(before, "\n}", "\n|> controller.get(\"/faq\", fn(ctx) { todo })\n}", 1)), 0o644)
	if _, err := NewPage(p, "blog", "faq", false, ""); err == nil || !strings.Contains(err.Error(), "/blog/faq") {
		t.Errorf("got %v", err)
	}
}

func TestPageValidatesNames(t *testing.T) {
	p := opened(t, scaffold.NoDatabase)
	for _, bad := range [][2]string{{"Blog", "about"}, {"blog", "About"}, {"blog", "about-us"}, {"type", "about"}, {"", "about"}} {
		if _, err := NewPage(p, bad[0], bad[1], false, ""); err == nil {
			t.Errorf("%v should be rejected", bad)
		}
	}
}

// 'howdy create pages' makes a controller that 'howdy create page' adds to.
// Its routes include "/:id", which would take "/stats" if it came first.
func TestPageJoinsPagesController(t *testing.T) {
	p := opened(t, scaffold.SQLite)
	c, err := NewSlice(p, "users", "user", []string{"name"})
	if err != nil {
		t.Fatal(err)
	}
	c.Pages, c.Routes = true, true
	if _, err := c.Write(); err != nil {
		t.Fatal(err)
	}
	if notes := c.Register(); len(notes) > 0 {
		t.Fatal(notes)
	}
	root := read(t, p, "src/demo_site.gleam")
	for _, want := range []string{"users_controller.controller(db)", "users_api_controller.controller(db)"} {
		if !strings.Contains(root, want) {
			t.Errorf("src/demo_site.gleam should register %s", want)
		}
	}
	pg := createPage(t, p, "users", "stats", true)
	if !pg.Exists || pg.URL() != "/users/stats" {
		t.Errorf("got %+v, URL %s", pg, pg.URL())
	}
	controller := read(t, p, "src/demo_site/users/users_controller.gleam")
	stats := strings.Index(controller, `controller.get("/stats", stats_page.show)`)
	socket := strings.Index(controller, `controller.get("/stats/live", stats_page.socket)`)
	id := strings.Index(controller, `controller.get("/:id"`)
	if stats < 0 || socket < 0 || id < 0 || stats > id || socket > id {
		t.Errorf("the page's routes should come before /:id:\n%s", controller)
	}
	if strings.Contains(read(t, p, "src/demo_site/users/users_api_controller.gleam"), "stats_page") {
		t.Error("the page should not be added to the JSON controller")
	}
	if _, err := NewPage(p, "users", "index", false, ""); err == nil {
		t.Error("the pages controller already has an index at /users")
	}
}

func TestPageLayouts(t *testing.T) {
	p := opened(t, scaffold.SQLite)
	if got := p.Layouts(); len(got) != 1 || got[0] != "default" {
		t.Fatalf("a new project should have the default layout, got %v", got)
	}
	if _, err := NewPage(p, "admin", "index", false, "admin"); err == nil ||
		!strings.Contains(err.Error(), "there is no admin layout") || !strings.Contains(err.Error(), "the project has: default") {
		t.Errorf("an unknown layout should be refused, got %v", err)
	}

	l, err := NewLayout(p, "admin_layout")
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Write(); err != nil {
		t.Fatal(err)
	}
	layout := read(t, p, "src/demo_site/layouts/admin_layout.gleam")
	for _, want := range []string{"//// The admin layout", "pub fn render(", "pub fn render_live(", `ui.link("/", [text("Demo site")])`} {
		if !strings.Contains(layout, want) {
			t.Errorf("admin_layout.gleam should contain %q", want)
		}
	}
	if _, err := NewLayout(p, "admin"); err == nil || !strings.Contains(err.Error(), "the admin layout already exists") {
		t.Errorf("a layout should not be created twice, got %v", err)
	}

	stats, err := NewPage(p, "admin", "stats", false, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stats.Write(); err != nil {
		t.Fatal(err)
	}
	page := read(t, p, "src/demo_site/admin/pages/stats_page.gleam")
	if !strings.Contains(page, "import demo_site/layouts/admin_layout\n") || !strings.Contains(page, "admin_layout.render(ctx,") {
		t.Errorf("the page should render through the admin layout:\n%s", page)
	}
}

// Projects made before layouts get the default one when a page needs it,
// and a layout that is there already is kept.
func TestDefaultLayoutWrittenWhenMissing(t *testing.T) {
	p := opened(t, scaffold.NoDatabase)
	path := filepath.Join(p.Dir, p.LayoutFile("default"))
	os.Remove(path)
	pg, err := NewPage(p, "blog", "about", false, "")
	if err != nil {
		t.Fatal(err)
	}
	written, err := pg.Write()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(written, " "), "src/demo_site/layouts/default_layout.gleam") {
		t.Errorf("the default layout should be written, wrote %v", written)
	}
	os.WriteFile(path, []byte("// mine\n"), 0o644)
	pg, _ = NewPage(p, "blog", "faq", false, "")
	if _, err := pg.Write(); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != "// mine\n" {
		t.Error("an existing default layout should be kept")
	}
}

func TestFeaturesCannotUseProjectFolders(t *testing.T) {
	p := opened(t, scaffold.SQLite)
	for _, name := range []string{"layouts", "platform", "migrations"} {
		if _, err := NewPage(p, name, "about", false, ""); err == nil {
			t.Errorf("a controller called %s should be refused", name)
		}
		if _, err := NewSlice(p, name, "thing", []string{"name"}); err == nil {
			t.Errorf("a slice called %s should be refused", name)
		}
	}
}
