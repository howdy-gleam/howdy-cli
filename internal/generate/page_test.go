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
	pg, err := NewPage(p, controller, name, live)
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
		if !strings.Contains(page, `page.new("About")`) || strings.Contains(page, "live") {
			t.Errorf("%s: unexpected page:\n%s", db.Label(), page)
		}
		test := read(t, p, "test/demo_site/blog/pages/about_page_test.gleam")
		app := "support.app(db)"
		if db == scaffold.NoDatabase {
			app = "demo_site.app()"
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
	if !strings.Contains(page, `live.mount("/blog/editor/live")`) || !strings.Contains(page, "pub fn socket(") {
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
		_, err := NewPage(p, c.controller, c.name, c.live)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s %s: got %v, want %q", c.controller, c.name, err, c.want)
		}
	}
	// A route added by hand counts too.
	path := filepath.Join(p.Dir, "src/demo_site/blog/blog_controller.gleam")
	os.WriteFile(path, []byte(strings.Replace(before, "\n}", "\n|> controller.get(\"/faq\", fn(ctx) { todo })\n}", 1)), 0o644)
	if _, err := NewPage(p, "blog", "faq", false); err == nil || !strings.Contains(err.Error(), "/blog/faq") {
		t.Errorf("got %v", err)
	}
}

func TestPageValidatesNames(t *testing.T) {
	p := opened(t, scaffold.NoDatabase)
	for _, bad := range [][2]string{{"Blog", "about"}, {"blog", "About"}, {"blog", "about-us"}, {"type", "about"}, {"", "about"}} {
		if _, err := NewPage(p, bad[0], bad[1], false); err == nil {
			t.Errorf("%v should be rejected", bad)
		}
	}
}
